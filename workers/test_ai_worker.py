import unittest
from unittest.mock import MagicMock, patch
import json
import datetime
from ai_worker import evaluate_alert

class TestAIWorkerHeuristics(unittest.TestCase):

    @patch("ai_worker.get_db_connection")
    def test_evaluate_no_history(self, mock_get_conn):
        # 1. Setup mocks
        mock_conn = MagicMock()
        mock_cur = MagicMock()
        mock_get_conn.return_value = mock_conn
        mock_conn.cursor.return_value.__enter__.return_value = mock_cur

        # Setup mock database response for fetching the alert
        mock_cur.fetchone.side_effect = [
            {
                "id": "alert-id-123",
                "tenant_id": "tenant-id-abc",
                "device_id": None,
                "event_type": "cpu",
                "severity": "critical",
                "status": "triggered",
                "summary": "High CPU load",
                "payload": {},
                "ai_analysis": {"host": "web-server-01"},
                "created_at": datetime.datetime.now()
            },
            [] # Historical search returns 1 element (itself)
        ]

        # Setup historical counts (fetchall returns only 1 row)
        mock_cur.fetchall.return_value = [
            {"id": "alert-id-123", "severity": "critical", "status": "triggered", "created_at": datetime.datetime.now()}
        ]

        mock_redis = MagicMock()

        # 2. Run
        evaluate_alert(mock_redis, "alert-id-123", "tenant-id-abc")

        # 3. Assertions
        # RLS context was set
        mock_cur.execute.assert_any_call("SET LOCAL app.current_tenant_id = %s", ("tenant-id-abc",))
        # Since count is 1 (<= 4), no updates should have occurred
        mock_conn.commit.assert_not_called()
        mock_redis.publish.assert_not_called()

    @patch("ai_worker.get_db_connection")
    def test_evaluate_flapping_suppression(self, mock_get_conn):
        mock_conn = MagicMock()
        mock_cur = MagicMock()
        mock_get_conn.return_value = mock_conn
        mock_conn.cursor.return_value.__enter__.return_value = mock_cur

        mock_cur.fetchone.return_value = {
            "id": "alert-id-123",
            "tenant_id": "tenant-id-abc",
            "device_id": None,
            "event_type": "cpu",
            "severity": "critical",
            "status": "triggered",
            "summary": "High CPU load",
            "payload": {"occurrences": 1.0},
            "ai_analysis": {"host": "web-server-01"},
            "created_at": datetime.datetime.now()
        }

        # Simulate 10 historical events in the last hour
        mock_cur.fetchall.return_value = [{"id": f"id-{i}"} for i in range(10)]

        mock_redis = MagicMock()

        # Run
        evaluate_alert(mock_redis, "alert-id-123", "tenant-id-abc")

        # Assertions
        # DB was updated to suppressed status
        update_call = mock_cur.execute.call_args_list[-1]
        query_str = update_call[0][0]
        params = update_call[0][1]

        self.assertIn("UPDATE alerts", query_str)
        self.assertEqual(params[0], "critical") # Severity unchanged
        self.assertEqual(params[1], "suppressed") # Status suppressed
        
        # Verify RLS was committed
        mock_conn.commit.assert_called_once()
        
        # Redis Pub/Sub notified
        mock_redis.publish.assert_called_once()
        published_payload = json.loads(mock_redis.publish.call_args[0][1])
        self.assertEqual(published_payload["status"], "suppressed")
        self.assertTrue(published_payload["ai_analysis"]["suppressed"])
        self.assertIn("Flapping noise", published_payload["ai_analysis"]["suppression_reason"])

    @patch("ai_worker.get_db_connection")
    def test_evaluate_severity_downgrade(self, mock_get_conn):
        mock_conn = MagicMock()
        mock_cur = MagicMock()
        mock_get_conn.return_value = mock_conn
        mock_conn.cursor.return_value.__enter__.return_value = mock_cur

        mock_cur.fetchone.return_value = {
            "id": "alert-id-123",
            "tenant_id": "tenant-id-abc",
            "device_id": None,
            "event_type": "cpu",
            "severity": "critical",
            "status": "triggered",
            "summary": "High CPU load",
            "payload": {"occurrences": 1.0},
            "ai_analysis": {"host": "web-server-01"},
            "created_at": datetime.datetime.now()
        }

        # Simulate 6 historical events in the last hour (between 5 and 8)
        mock_cur.fetchall.return_value = [{"id": f"id-{i}"} for i in range(6)]

        mock_redis = MagicMock()

        # Run
        evaluate_alert(mock_redis, "alert-id-123", "tenant-id-abc")

        # Assertions
        # DB was updated: severity downgraded from critical -> warning
        update_call = mock_cur.execute.call_args_list[-1]
        params = update_call[0][1]

        self.assertEqual(params[0], "warning") # Downgraded severity
        self.assertEqual(params[1], "triggered") # Status unchanged
        mock_conn.commit.assert_called_once()
        mock_redis.publish.assert_called_once()

        published_payload = json.loads(mock_redis.publish.call_args[0][1])
        self.assertEqual(published_payload["severity"], "warning")
        self.assertTrue(published_payload["ai_analysis"]["downgraded"])
        self.assertIn("Severity downgraded", published_payload["ai_analysis"]["downgrade_reason"])

if __name__ == "__main__":
    unittest.main()
