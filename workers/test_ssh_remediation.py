import unittest
from unittest.mock import MagicMock, patch
import io
import sys
from ssh_remediation import execute_remediation, fetch_ssh_credentials

class TestSSHRemediation(unittest.TestCase):

    @patch("ssh_remediation.get_db_connection")
    @patch("security_crypto.get_master_key")
    @patch("security_crypto.decrypt")
    def test_fetch_ssh_credentials(self, mock_decrypt, mock_get_key, mock_get_conn):
        mock_conn = MagicMock()
        mock_cur = MagicMock()
        mock_get_conn.return_value = mock_conn
        mock_conn.cursor.return_value.__enter__.return_value = mock_cur
        
        mock_get_key.return_value = b"masterkeymasterkeymasterkey32"
        mock_decrypt.side_effect = [
            b"ssh_user_abc",
            b"secretpassword",
            b"ssh_private_key_data",
            b"22"
        ]

        # Return mock database records for keys
        mock_cur.fetchone.side_effect = [
            {"encrypted_value": b"enc_user", "nonce": b"nonce_u"},
            {"encrypted_value": b"enc_pass", "nonce": b"nonce_p"},
            {"encrypted_value": b"enc_key", "nonce": b"nonce_k"},
            {"encrypted_value": b"enc_port", "nonce": b"nonce_pt"}
        ]

        credentials = fetch_ssh_credentials("tenant-uuid-123")
        
        self.assertEqual(credentials["ssh_username"], "ssh_user_abc")
        self.assertEqual(credentials["ssh_password"], "secretpassword")
        self.assertEqual(credentials["ssh_private_key"], "ssh_private_key_data")
        self.assertEqual(credentials["ssh_port"], 22)

    @patch("ssh_remediation.fetch_alert_details")
    @patch("ssh_remediation.fetch_ssh_credentials")
    @patch("paramiko.SSHClient")
    def test_execute_remediation_real_ssh_success(self, mock_ssh_client, mock_fetch_creds, mock_fetch_alert):
        mock_fetch_alert.return_value = {
            "event_type": "cpu",
            "ai_analysis": {"host": "192.168.1.50"}
        }
        mock_fetch_creds.return_value = {
            "ssh_username": "ops_user",
            "ssh_password": "supersecretpassword",
            "ssh_port": 22
        }

        # Mock SSH Client behavior
        mock_ssh = MagicMock()
        mock_ssh_client.return_value = mock_ssh
        mock_stdout = MagicMock()
        mock_stderr = MagicMock()
        mock_stdout.channel.recv_exit_status.return_value = 0
        mock_stdout.read.return_value = b"nginx restarted successfully"
        mock_stderr.read.return_value = b""
        mock_ssh.exec_command.return_value = (None, mock_stdout, mock_stderr)

        # Capture output
        captured_output = io.StringIO()
        sys.stdout = captured_output

        try:
            execute_remediation("tenant-uuid-123", "alert-uuid-999")
        finally:
            sys.stdout = sys.__stdout__

        output = captured_output.getvalue()
        self.assertIn("[SSH SUCCESS] Connection established successfully.", output)
        self.assertIn("Command executed: sudo systemctl restart nginx", output)
        self.assertIn("Exit Code: 0", output)
        self.assertIn("nginx restarted successfully", output)

    @patch("ssh_remediation.fetch_alert_details")
    @patch("ssh_remediation.fetch_ssh_credentials")
    def test_execute_remediation_simulator_mode(self, mock_fetch_creds, mock_fetch_alert):
        mock_fetch_alert.return_value = {
            "event_type": "memory",
            "ai_analysis": {"host": "db-server-02"}
        }
        mock_fetch_creds.return_value = {
            "ssh_username": "mock_ssh", # triggers simulation
            "ssh_port": 22
        }

        captured_output = io.StringIO()
        sys.stdout = captured_output

        try:
            execute_remediation("tenant-uuid-123", "alert-uuid-999")
        finally:
            sys.stdout = sys.__stdout__

        output = captured_output.getvalue()
        self.assertIn("[SSH SIMULATOR] Connection established to target host: db-server-02", output)
        self.assertIn("Running command: sudo sync && sudo sysctl -w vm.drop_caches=3", output)
        self.assertIn("System memory cache cleared successfully.", output)
        self.assertIn("Exit Code: 0", output)

if __name__ == "__main__":
    unittest.main()
