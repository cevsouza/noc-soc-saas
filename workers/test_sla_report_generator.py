import unittest
import os
from datetime import datetime, timedelta
from sla_report_generator import generate_pdf

class TestSLAReportGenerator(unittest.TestCase):

    def test_generate_pdf_creates_file(self):
        output_file = "test_sla_report.pdf"
        
        # Ensure file does not exist
        if os.path.exists(output_file):
            os.remove(output_file)

        # Mock alert records
        now = datetime.now()
        mock_alerts = [
            {
                "id": "alert-1",
                "severity": "critical",
                "status": "resolved",
                "event_type": "cpu",
                "summary": "High CPU utilization on web server",
                "created_at": now - timedelta(hours=2),
                # NEW: real acknowledged_at so the (previously fabricated) MTTA path is
                # actually exercised with a real timestamp delta.
                "acknowledged_at": now - timedelta(hours=2) + timedelta(minutes=8),
                "resolved_at": now - timedelta(hours=1)
            },
            {
                "id": "alert-2",
                "severity": "fatal",
                "status": "triggered",
                "event_type": "memory",
                "summary": "OOM Killer triggered on database node",
                "created_at": now - timedelta(hours=4),
                # Deliberately no "acknowledged_at" key at all — still exercises the .get()
                # safe-access path for alerts that haven't been acknowledged yet.
                "resolved_at": None
            }
        ]

        try:
            generate_pdf(
                tenant_id="tenant-123",
                tenant_name="Test Enterprise Corp",
                alerts=mock_alerts,
                output_path=output_file
            )

            # Assert file exists and is not empty
            self.assertTrue(os.path.exists(output_file))
            self.assertGreater(os.path.getsize(output_file), 1000) # PDF should be at least a few KB
        finally:
            # Clean up
            if os.path.exists(output_file):
                os.remove(output_file)

if __name__ == "__main__":
    unittest.main()
