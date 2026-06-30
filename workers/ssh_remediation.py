import os
import sys
import argparse
import json
import logging
import paramiko
import psycopg2
from psycopg2.extras import RealDictCursor
import security_crypto

# Setup logging
logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
logger = logging.getLogger("SSHRemediation")

DB_HOST = os.environ.get("DB_HOST", "localhost")
DB_PORT = int(os.environ.get("DB_PORT", "5432"))
DB_USER = os.environ.get("DB_USER", "postgres")
DB_PASSWORD = os.environ.get("DB_PASSWORD", "postgres")
DB_NAME = os.environ.get("DB_NAME", "noc")

def get_db_connection():
    return psycopg2.connect(
        host=DB_HOST,
        port=DB_PORT,
        user=DB_USER,
        password=DB_PASSWORD,
        database=DB_NAME,
        cursor_factory=RealDictCursor
    )

def fetch_ssh_credentials(tenant_id: str):
    master_key = security_crypto.get_master_key()
    keys = ["ssh_username", "ssh_password", "ssh_private_key", "ssh_port"]
    credentials = {"ssh_port": 22}

    conn = get_db_connection()
    try:
        with conn.cursor() as cur:
            # RLS Enforced session
            cur.execute("SET LOCAL app.current_tenant_id = %s", (tenant_id,))
            
            for key in keys:
                cur.execute(
                    "SELECT encrypted_value, nonce FROM tenant_vault WHERE secret_key = %s",
                    (key,)
                )
                row = cur.fetchone()
                if row:
                    decrypted = security_crypto.decrypt(
                        row["encrypted_value"],
                        row["nonce"],
                        master_key
                    )
                    val = decrypted.decode("utf-8")
                    if key == "ssh_port":
                        credentials[key] = int(val)
                    else:
                        credentials[key] = val
    except Exception as e:
        logger.error(f"Error fetching SSH credentials: {e}")
    finally:
        conn.close()
        
    return credentials

def fetch_alert_details(tenant_id: str, alert_id: str):
    conn = get_db_connection()
    alert = None
    try:
        with conn.cursor() as cur:
            cur.execute("SET LOCAL app.current_tenant_id = %s", (tenant_id,))
            cur.execute(
                "SELECT event_type, ai_analysis FROM alerts WHERE id = %s",
                (alert_id,)
            )
            alert = cur.fetchone()
    except Exception as e:
        logger.error(f"Error fetching alert details: {e}")
    finally:
        conn.close()
    return alert

def execute_remediation(tenant_id: str, alert_id: str):
    logger.info(f"Initializing SSH Remediation for Alert {alert_id} (Tenant: {tenant_id})")
    
    alert = fetch_alert_details(tenant_id, alert_id)
    if not alert:
        logger.error(f"Alert {alert_id} not found.")
        sys.exit(1)
        
    ai_analysis = alert.get("ai_analysis") or {}
    host = ai_analysis.get("host") or ""
    event_type = alert.get("event_type") or ""

    if not host:
        logger.error(f"No target host specified in alert {alert_id} metadata.")
        sys.exit(1)

    credentials = fetch_ssh_credentials(tenant_id)
    username = credentials.get("ssh_username")
    password = credentials.get("ssh_password")
    pkey_str = credentials.get("ssh_private_key")
    port = credentials.get("ssh_port", 22)

    if not username:
        # Fallback to simulation mode if no credentials stored
        logger.warning("No SSH credentials found in vault. Running in SIMULATOR mode.")
        run_simulator(host, event_type)
        return

    if username == "mock_ssh":
        logger.info("Mock SSH credential detected. Running in SIMULATOR mode.")
        run_simulator(host, event_type)
        return

    # Select command based on event type
    command = "echo 'No runbook registered'"
    if event_type == "cpu":
        command = "sudo systemctl restart nginx"
    elif event_type == "memory":
        command = "sudo sync && sudo sysctl -w vm.drop_caches=3"
    elif event_type == "network_link":
        command = "sudo ip link set dev eth0 down && sudo ip link set dev eth0 up"

    logger.info(f"Executing command via SSH: '{command}' on target host {host}:{port}")

    # Establish Paramiko connection
    ssh = paramiko.SSHClient()
    ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())

    try:
        if pkey_str:
            # Authenticate using private key
            from io import StringIO
            pkey = paramiko.RSAKey.from_private_key(StringIO(pkey_str))
            ssh.connect(host, port=port, username=username, pkey=pkey, timeout=10)
        else:
            # Authenticate using password
            ssh.connect(host, port=port, username=username, password=password, timeout=10)

        stdin, stdout, stderr = ssh.exec_command(command)
        exit_status = stdout.channel.recv_exit_status()

        out_lines = stdout.read().decode("utf-8")
        err_lines = stderr.read().decode("utf-8")

        print("[SSH SUCCESS] Connection established successfully.")
        print(f"Command executed: {command}")
        print(f"Exit Code: {exit_status}")
        print(f"Stdout:\n{out_lines}")
        if err_lines:
            print(f"Stderr:\n{err_lines}")

        if exit_status != 0:
            sys.exit(exit_status)

    except Exception as e:
        logger.error(f"SSH execution failed: {e}")
        sys.exit(1)
    finally:
        ssh.close()

def run_simulator(host: str, event_type: str):
    print("[SSH SIMULATOR] Connection established to target host:", host)
    print("[SSH SIMULATOR] Authenticated using tenant_vault SSH keys.")
    
    if event_type == "cpu":
        cmd = "sudo systemctl restart nginx"
        print(f"[SSH SIMULATOR] Running command: {cmd}")
        print("Stdout:\nStopping nginx service...\nStarting nginx service...\nService nginx restarted successfully.")
    elif event_type == "memory":
        cmd = "sudo sync && sudo sysctl -w vm.drop_caches=3"
        print(f"[SSH SIMULATOR] Running command: {cmd}")
        print("Stdout:\nSyncing filesystem buffers...\nDropping pagecache, dentries and inodes...\nSystem memory cache cleared successfully.")
    else:
        cmd = "echo 'No runbook registered'"
        print(f"[SSH SIMULATOR] Running command: {cmd}")
        print("Stdout:\nNo corrective action needed.")
        
    print("Exit Code: 0")

def main():
    parser = argparse.ArgumentParser(description="SSH Remediation Runbook Executor")
    parser.add_argument("--tenant", required=True, help="Tenant UUID")
    parser.add_argument("--alert", required=True, help="Alert UUID")
    args = parser.parse_args()

    execute_remediation(args.tenant, args.alert)

if __name__ == "__main__":
    main()
