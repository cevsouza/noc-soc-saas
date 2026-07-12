import os
import sys
import argparse
import json
import logging
from datetime import datetime, timedelta
import psycopg2
from psycopg2.extras import RealDictCursor

# ReportLab Imports for Premium PDF Generation
from reportlab.lib.pagesizes import letter
from reportlab.platypus import SimpleDocTemplate, Paragraph, Spacer, Table, TableStyle
from reportlab.lib.styles import getSampleStyleSheet, ParagraphStyle
from reportlab.lib import colors

# Setup logging
logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")
logger = logging.getLogger("SLAGenerator")

# Mirrors frontend/src/components/alerts/sla-countdown.tsx (lines 19-24) and the equivalent Go
# map in internal/api/handler.go (slaTargetMinutesBySeverity). Duplicated here because this
# script runs in a separate Python process/runtime and can't import Go or TS source — if the
# targets ever change, update all three places.
SLA_TARGET_MINUTES_BY_SEVERITY = {
    "fatal": 15,
    "critical": 30,
    "warning": 120,
    "info": 480,
}
DEFAULT_SLA_TARGET_MINUTES = 480  # falls back to the "info" tier for any unrecognized severity

DB_HOST = os.environ.get("DB_HOST", "localhost")
DB_PORT = int(os.environ.get("DB_PORT", "5432"))
DB_USER = os.environ.get("DB_USER", "postgres")
DB_PASSWORD = os.environ.get("DB_PASSWORD", "postgres")
DB_NAME = os.environ.get("DB_NAME", "noc")

def get_db_connection():
    db_url = os.environ.get("DATABASE_URL")
    if db_url:
        return psycopg2.connect(db_url, cursor_factory=RealDictCursor)
    return psycopg2.connect(
        host=DB_HOST,
        port=DB_PORT,
        user=DB_USER,
        password=DB_PASSWORD,
        database=DB_NAME,
        cursor_factory=RealDictCursor
    )

def fetch_sla_data(tenant_id: str):
    conn = get_db_connection()
    alerts = []
    try:
        with conn.cursor() as cur:
            cur.execute("SET LOCAL app.current_tenant_id = %s", (tenant_id,))
            cur.execute(
                """
                SELECT id, event_type, severity, status, summary, created_at, updated_at,
                       acknowledged_at, resolved_at
                FROM alerts
                WHERE created_at >= NOW() - INTERVAL '30 days'
                ORDER BY created_at DESC
                """
            )
            alerts = cur.fetchall()
    except Exception as e:
        logger.error(f"Error fetching SLA data: {e}")
    finally:
        conn.close()
    return alerts

def generate_pdf(tenant_id: str, tenant_name: str, alerts: list, output_path: str):
    # Setup document
    doc = SimpleDocTemplate(output_path, pagesize=letter, rightMargin=40, leftMargin=40, topMargin=40, bottomMargin=40)
    story = []
    styles = getSampleStyleSheet()

    # Premium Color Palette
    primary_color = colors.HexColor("#6d28d9")   # Violet
    secondary_color = colors.HexColor("#0f172a") # Dark Slate
    text_color = colors.HexColor("#334155")      # Light Slate Text
    border_color = colors.HexColor("#cbd5e1")    # Light Gray Border
    success_color = colors.HexColor("#10b981")   # Emerald Green

    # Styles
    title_style = ParagraphStyle(
        'DocTitle',
        parent=styles['Heading1'],
        fontName='Helvetica-Bold',
        fontSize=24,
        textColor=primary_color,
        spaceAfter=6
    )
    subtitle_style = ParagraphStyle(
        'DocSubtitle',
        parent=styles['Normal'],
        fontName='Helvetica',
        fontSize=10,
        textColor=text_color,
        spaceAfter=20
    )
    section_style = ParagraphStyle(
        'SectionHeading',
        parent=styles['Heading2'],
        fontName='Helvetica-Bold',
        fontSize=14,
        textColor=secondary_color,
        spaceBefore=15,
        spaceAfter=10
    )
    body_style = ParagraphStyle(
        'BodyText',
        parent=styles['Normal'],
        fontName='Helvetica',
        fontSize=10,
        textColor=text_color,
        leading=14
    )
    table_header_style = ParagraphStyle(
        'TableHeader',
        parent=styles['Normal'],
        fontName='Helvetica-Bold',
        fontSize=9,
        textColor=colors.white
    )
    table_cell_style = ParagraphStyle(
        'TableCell',
        parent=styles['Normal'],
        fontName='Helvetica',
        fontSize=9,
        textColor=text_color
    )

    # 1. Header Section
    story.append(Paragraph("SLA Compliance & Service Delivery Report", title_style))
    story.append(Paragraph(f"Tenant Account: {tenant_name} | Generated on: {datetime.now().strftime('%Y-%m-%d %H:%M:%S UTC')}", subtitle_style))
    story.append(Spacer(1, 10))

    # Compute Statistics
    total_alerts = len(alerts)
    fatal_alerts = sum(1 for a in alerts if a['severity'] == 'fatal')
    critical_alerts = sum(1 for a in alerts if a['severity'] == 'critical')
    resolved_alerts = sum(1 for a in alerts if a['status'] == 'resolved')
    
    # Calculate MTTA & MTTR in minutes from real timestamps (previously MTTA was fabricated —
    # min(15, mttr*0.15) or a hardcoded 8.5 — because acknowledged_at wasn't even fetched from
    # the DB). Uses .get() throughout since some callers (e.g. the test suite's mock data) omit
    # acknowledged_at/resolved_at entirely rather than setting them to None.
    mtta_list = []
    mttr_list = []
    compliance_hits = 0
    compliance_total = 0

    for a in alerts:
        c_time = a['created_at']
        ack_time = a.get('acknowledged_at')
        r_time = a.get('resolved_at')

        if ack_time:
            mtta_list.append((ack_time - c_time).total_seconds() / 60.0)

        if r_time:
            delta_resolve = (r_time - c_time).total_seconds() / 60.0
            mttr_list.append(delta_resolve)

            # SLA compliance only counts already-resolved incidents (an open incident hasn't
            # "passed or failed" its target yet) — mirrors the Go endpoint's same convention.
            target = SLA_TARGET_MINUTES_BY_SEVERITY.get(a.get('severity'), DEFAULT_SLA_TARGET_MINUTES)
            compliance_total += 1
            if delta_resolve <= target:
                compliance_hits += 1

    # No fabricated fallback numbers — 0.0/100% honestly communicate "no data" instead of
    # inventing a plausible-looking value.
    mtta = sum(mtta_list) / len(mtta_list) if mtta_list else 0.0
    mttr = sum(mttr_list) / len(mttr_list) if mttr_list else 0.0
    sla_compliance = (compliance_hits / compliance_total * 100.0) if compliance_total else 100.0

    # 2. Executive Summary Metrics Table
    summary_data = [
        [
            Paragraph("<b>Total Incidents</b>", body_style),
            Paragraph("<b>Resolved Rate</b>", body_style),
            Paragraph("<b>Avg MTTA</b>", body_style),
            Paragraph("<b>Avg MTTR</b>", body_style),
            Paragraph("<b>SLA Compliance</b>", body_style)
        ],
        [
            Paragraph(f"{total_alerts}", body_style),
            Paragraph(f"{((resolved_alerts/total_alerts)*100):.1f}%" if total_alerts > 0 else "100%", body_style),
            Paragraph(f"{mtta:.1f} min", body_style),
            Paragraph(f"{mttr:.1f} min", body_style),
            Paragraph(f"<font color='#10b981'><b>{sla_compliance:.2f}%</b></font>" if sla_compliance >= 99.0 else f"<font color='#f59e0b'><b>{sla_compliance:.2f}%</b></font>", body_style)
        ]
    ]
    
    t_summary = Table(summary_data, colWidths=[100, 100, 100, 100, 130])
    t_summary.setStyle(TableStyle([
        ('BACKGROUND', (0,0), (-1,0), colors.HexColor("#f1f5f9")),
        ('ALIGN', (0,0), (-1,-1), 'CENTER'),
        ('VALIGN', (0,0), (-1,-1), 'MIDDLE'),
        ('BOTTOMPADDING', (0,0), (-1,-1), 8),
        ('TOPPADDING', (0,0), (-1,-1), 8),
        ('GRID', (0,0), (-1,-1), 1, border_color),
    ]))
    
    story.append(Paragraph("Executive SLA Metrics Summary (Last 30 Days)", section_style))
    story.append(t_summary)
    story.append(Spacer(1, 20))

    # 3. Incident List Details Table
    story.append(Paragraph("Incident Ledger (Recent Alerts)", section_style))
    
    ledger_headers = [
        Paragraph("<b>Severity</b>", table_header_style),
        Paragraph("<b>Event Type</b>", table_header_style),
        Paragraph("<b>Summary / Message</b>", table_header_style),
        Paragraph("<b>Timestamp (UTC)</b>", table_header_style),
        Paragraph("<b>Status</b>", table_header_style)
    ]
    
    ledger_data = [ledger_headers]
    
    # Show last 10 alerts in detail
    display_alerts = alerts[:10]
    if not display_alerts:
        ledger_data.append([
            Paragraph("No incidents", table_cell_style),
            Paragraph("N/A", table_cell_style),
            Paragraph("No active or resolved incidents recorded in this timeframe.", table_cell_style),
            Paragraph("-", table_cell_style),
            Paragraph("CLEAN", table_cell_style)
        ])
    else:
        for a in display_alerts:
            created_str = a['created_at'].strftime('%Y-%m-%d %H:%M')
            
            severity_label = a['severity'].upper()
            status_label = a['status'].upper()
            
            ledger_data.append([
                Paragraph(f"<b>{severity_label}</b>", table_cell_style),
                Paragraph(a['event_type'], table_cell_style),
                Paragraph(a['summary'], table_cell_style),
                Paragraph(created_str, table_cell_style),
                Paragraph(status_label, table_cell_style)
            ])
        
    t_ledger = Table(ledger_data, colWidths=[70, 90, 200, 100, 70])
    t_ledger.setStyle(TableStyle([
        ('BACKGROUND', (0,0), (-1,0), primary_color),
        ('ALIGN', (0,0), (-1,-1), 'LEFT'),
        ('VALIGN', (0,0), (-1,-1), 'MIDDLE'),
        ('BOTTOMPADDING', (0,0), (-1,-1), 6),
        ('TOPPADDING', (0,0), (-1,-1), 6),
        ('ROWBACKGROUNDS', (0,1), (-1,-1), [colors.white, colors.HexColor("#f8fafc")]),
        ('GRID', (0,0), (-1,-1), 0.5, border_color),
    ]))
    story.append(t_ledger)
    
    if len(alerts) > 10:
        story.append(Spacer(1, 10))
        story.append(Paragraph(f"<i>* Showing latest 10 of {len(alerts)} total incidents registered in reporting timeframe.</i>", subtitle_style))

    # Build PDF Document
    doc.build(story)
    logger.info(f"SLA PDF generated successfully at: {output_path}")

def generate_mock_alerts():
    # Helper to generate beautiful mock database rows if DB is empty
    mock_data = []
    now = datetime.now()
    event_types = ["cpu", "memory", "network_link", "auth_failure", "disk_full"]
    summaries = [
        "High CPU utilization detected on nginx router node",
        "OOM-killer triggered: database instance low memory warning",
        "Port bounced: packet loss observed on border network switch",
        "Failed login: multiple validation errors on user root",
        "Disk capacity alert: partition /dev/sda2 usage exceeded 90%"
    ]
    severities = ["critical", "warning", "info", "fatal", "critical"]
    statuses = ["resolved", "resolved", "resolved", "triggered", "resolved"]
    
    for i in range(15):
        c_time = now - timedelta(days=float(i)*0.8, hours=float(i)*1.5)
        # 80% are resolved
        resolved = statuses[i % len(statuses)] == "resolved"
        r_time = c_time + timedelta(minutes=float(45 + i*8)) if resolved else None
        
        mock_data.append({
            "id": f"alert-uuid-{i}",
            "severity": severities[i % len(severities)],
            "status": statuses[i % len(statuses)],
            "event_type": event_types[i % len(event_types)],
            "summary": summaries[i % len(summaries)],
            "created_at": c_time,
            "resolved_at": r_time
        })
    return mock_data

def main():
    parser = argparse.ArgumentParser(description="Monthly SLA Report PDF Generator")
    parser.add_argument("--tenant", required=True, help="Tenant UUID")
    parser.add_argument("--name", required=True, help="Tenant Name")
    parser.add_argument("--output", required=True, help="Output PDF File Path")
    args = parser.parse_args()

    # Create destination directory if not exists
    out_dir = os.path.dirname(args.output)
    if out_dir and not os.path.exists(out_dir):
        os.makedirs(out_dir)

    alerts = fetch_sla_data(args.tenant)
    generate_pdf(args.tenant, args.name, alerts, args.output)

if __name__ == "__main__":
    main()
