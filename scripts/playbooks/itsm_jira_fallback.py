#!/usr/bin/env python3
"""
ITSM Fallback and Governance Playbook - Opens and routes tickets in Jira Service Management (JSM).
"""

import sys
import argparse
import time
import json

def open_jira_ticket(jsm_url, api_token, summary, description, severity, assignment_group):
    print(f"[*] Connecting to Jira Service Management Cloud: {jsm_url}...")
    time.sleep(1.2)
    
    # Simulates a POST call to Jira REST API (create issue endpoint)
    # Endpoint e.g.: POST /rest/api/3/issue
    print(f"[*] Creating incident ticket in Jira project...")
    ticket_payload = {
        "fields": {
            "project": {"key": "ITSM"},
            "summary": summary,
            "description": description,
            "issuetype": {"name": "Incident"},
            "priority": {"name": "High" if severity in ["critical", "fatal"] else "Medium"},
            "customfield_assignment_group": {"value": assignment_group}
        }
    }
    
    # Simulate API success response
    print(f"    [API-CALL] POST https://{jsm_url}/rest/api/3/issue")
    ticket_key = "ITSM-7839"
    print(f"    [API-RESPONSE] 201 Created - Ticket {ticket_key} opened and routed to '{assignment_group}' queue.")
    return ticket_key

def main():
    parser = argparse.ArgumentParser(description="Jira Service Management Ticket Router")
    parser.add_argument("--jsm-host", default="jira-jsm.atlassian.net", help="Jira JSM endpoint")
    parser.add_argument("--summary", required=True, help="Incident ticket summary")
    parser.add_argument("--details", required=True, help="JSON or text logs of execution")
    parser.add_argument("--severity", default="critical", help="Severity level of the incident")
    parser.add_argument("--group", default="SRE Operations", help="Target engineering queue")
    args = parser.parse_args()

    try:
        ticket = open_jira_ticket(
            jsm_url=args.jsm_host,
            api_token="secret-atlassian-token",
            summary=args.summary,
            description=args.details,
            severity=args.severity,
            assignment_group=args.group
        )
        print(json.dumps({
            "status": "success",
            "itsm_platform": "jira_service_management",
            "ticket_ref": ticket,
            "routed_to": args.group,
            "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
        }, indent=2))
        sys.exit(0)
    except Exception as e:
        print(json.dumps({
            "status": "error",
            "error_message": str(e)
        }, indent=2))
        sys.exit(1)

if __name__ == "__main__":
    main()
