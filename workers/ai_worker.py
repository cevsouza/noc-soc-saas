import os
import json
import logging
import time
from typing import Optional
import redis
import psycopg2
from psycopg2.extras import RealDictCursor

# Setup logging
logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
logger = logging.getLogger("AIWorker")

# Load Configurations
REDIS_HOST = os.environ.get("REDIS_HOST", "localhost")
REDIS_PORT = int(os.environ.get("REDIS_PORT", "6379"))
REDIS_PASSWORD = os.environ.get("REDIS_PASSWORD", None)
REDIS_DB = int(os.environ.get("REDIS_DB", "0"))

DB_HOST = os.environ.get("DB_HOST", "localhost")
DB_PORT = int(os.environ.get("DB_PORT", "5432"))
DB_USER = os.environ.get("DB_USER", "postgres")
DB_PASSWORD = os.environ.get("DB_PASSWORD", "postgres")
DB_NAME = os.environ.get("DB_NAME", "noc")

AI_EVAL_QUEUE = "noc:queue:ai_evaluation"
PUBSUB_CHANNEL_PREFIX = "noc:pubsub:tenant:"

def get_db_connection():
    return psycopg2.connect(
        host=DB_HOST,
        port=DB_PORT,
        user=DB_USER,
        password=DB_PASSWORD,
        database=DB_NAME,
        cursor_factory=RealDictCursor
    )

def evaluate_alert(redis_client, alert_id: str, tenant_id: str):
    conn = None
    try:
        conn = get_db_connection()
        with conn.cursor() as cur:
            # Enforce Row-Level Security (RLS) within this transaction
            cur.execute("SET LOCAL app.current_tenant_id = %s", (tenant_id,))

            # 1. Fetch the alert
            cur.execute(
                "SELECT id, tenant_id, device_id, event_type, severity, status, summary, payload, ai_analysis, created_at FROM alerts WHERE id = %s",
                (alert_id,)
            )
            alert = cur.fetchone()
            if not alert:
                logger.warning(f"Alert {alert_id} not found in database for tenant {tenant_id}. Skipping.")
                return

            ai_analysis = alert.get("ai_analysis") or {}
            host = ai_analysis.get("host") or ""
            event_type = alert.get("event_type")

            if not host or not event_type:
                logger.info(f"Alert {alert_id} has no host or event_type. Skipping AI evaluation.")
                return

            # 2. Query historical frequency of the same event_type and host in the last 1 hour
            # Note: RLS is active, so we can only read alerts of this tenant.
            cur.execute(
                """
                SELECT id, severity, status, created_at 
                FROM alerts 
                WHERE event_type = %s 
                  AND ai_analysis->>'host' = %s 
                  AND created_at >= NOW() - INTERVAL '1 hour'
                """,
                (event_type, host)
            )
            history = cur.fetchall()
            count = len(history)

            logger.info(f"Alert {alert_id} (Type: {event_type}, Host: {host}) has {count} occurrences in the last hour.")

            updated = False
            original_severity = alert.get("severity")
            original_status = alert.get("status")

            # 3. Apply Heuristics
            # Heuristic A: Flapping / Heavy noise suppression (N > 8 times in 1 hour)
            if count > 8:
                ai_analysis["suppressed"] = True
                ai_analysis["suppression_reason"] = f"Flapping noise: occurred {count} times in the last hour"
                alert["status"] = "suppressed"
                alert["ai_analysis"] = ai_analysis
                updated = True
                logger.info(f"Suppressing alert {alert_id} due to flapping (Count: {count})")

            # Heuristic B: Mild frequency severity downgrade (4 < N <= 8 times in 1 hour)
            elif count > 4:
                new_severity = original_severity
                if original_severity == "fatal":
                    new_severity = "critical"
                elif original_severity == "critical":
                    new_severity = "warning"
                elif original_severity == "warning":
                    new_severity = "info"

                if new_severity != original_severity:
                    ai_analysis["downgraded"] = True
                    ai_analysis["downgrade_reason"] = f"Severity downgraded from {original_severity} to {new_severity} due to frequency ({count} times/hour)"
                    alert["severity"] = new_severity
                    alert["ai_analysis"] = ai_analysis
                    updated = True
                    logger.info(f"Downgrading severity for alert {alert_id} from {original_severity} to {new_severity}")

            # 4. Save updates back to PostgreSQL and publish to Redis Pub/Sub
            if updated:
                cur.execute(
                    """
                    UPDATE alerts 
                    SET severity = %s, status = %s, ai_analysis = %s, updated_at = NOW() 
                    WHERE id = %s
                    """,
                    (alert["severity"], alert["status"], json.dumps(ai_analysis), alert_id)
                )
                conn.commit()

                # Publish updated alert to Redis Pub/Sub so that Cockpit is updated in real-time
                # Convert timestamps to string for JSON serialization compatibility
                alert_payload = dict(alert)
                alert_payload["id"] = str(alert_payload["id"])
                alert_payload["tenant_id"] = str(alert_payload["tenant_id"])
                if alert_payload.get("device_id"):
                    alert_payload["device_id"] = str(alert_payload["device_id"])
                alert_payload["created_at"] = alert_payload["created_at"].isoformat()
                alert_payload["updated_at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

                channel = f"{PUBSUB_CHANNEL_PREFIX}{tenant_id}"
                redis_client.publish(channel, json.dumps(alert_payload))
                logger.info(f"Published alert update for {alert_id} to channel {channel}")
            else:
                logger.info(f"Alert {alert_id} evaluated: no suppression or downgrade needed.")

    except Exception as e:
        logger.error(f"Error evaluating alert {alert_id}: {e}", exc_info=True)
        if conn:
            conn.rollback()
    finally:
        if conn:
            conn.close()

def main():
    logger.info("Initializing NOC/SOC Python AI & Heuristic Worker...")
    
    # Establish Redis connection
    try:
        redis_client = redis.Redis(
            host=REDIS_HOST,
            port=REDIS_PORT,
            password=REDIS_PASSWORD,
            db=REDIS_DB
        )
        redis_client.ping()
        logger.info("Connected to Redis successfully.")
    except Exception as e:
        logger.critical(f"Failed to connect to Redis: {e}")
        return

    logger.info(f"AI Worker listening on queue '{AI_EVAL_QUEUE}'...")
    
    # Main Worker Loop
    while True:
        try:
            # BRPOP blocks until an item is available
            # Timeout is set to 5s to allow graceful exit checking
            result = redis_client.brpop(AI_EVAL_QUEUE, timeout=5)
            if not result:
                continue

            # brpop returns a tuple: (queue_name, element_value)
            _, raw_payload = result
            payload = json.loads(raw_payload.decode("utf-8"))

            alert_id = payload.get("alert_id")
            tenant_id = payload.get("tenant_id")

            if alert_id and tenant_id:
                evaluate_alert(redis_client, alert_id, tenant_id)

        except KeyboardInterrupt:
            logger.info("AI Worker shutting down gracefully.")
            break
        except Exception as e:
            logger.error(f"Error in main loop: {e}")
            time.sleep(2) # Prevent tight crash loop

if __name__ == "__main__":
    main()
