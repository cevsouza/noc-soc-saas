#!/usr/bin/env python3
"""
SOC SOAR Containment Playbook - Network Firewall Blocking & EDR Host Isolation.
"""

import sys
import argparse
import json
import time

def block_ip_on_firewall(firewall_ip, api_key, malicious_ip):
    print(f"[*] Connecting to Firewall Security Appliance: {firewall_ip}...")
    time.sleep(1)
    
    # Simulates a REST API call to block an IP
    # Endpoint e.g.: POST /api/v2/cmdb/firewall/address
    print(f"[*] Posting address block rule for IP: {malicious_ip}...")
    payload = {
        "name": f"SOAR_BLOCKED_{malicious_ip}",
        "type": "ipmask",
        "subnet": f"{malicious_ip} 255.255.255.255",
        "comment": "Blocked automatically by SOC SOAR Engine"
    }
    
    # Simulate API success response
    print(f"    [API-CALL] POST https://{firewall_ip}/api/v2/cmdb/firewall/address")
    print(f"    [API-RESPONSE] 201 Created - Rule added successfully.")
    return True

def isolate_endpoint_edr(edr_console, edr_token, device_hostname):
    print(f"[*] Connecting to EDR Console: {edr_console}...")
    time.sleep(1.2)
    
    # Simulates EDR API Network Isolation call
    # Endpoint e.g.: POST /devices/entities/devices-actions/v2?action_name=contain
    print(f"[*] Requesting host network containment/isolation for host: '{device_hostname}'...")
    print(f"    [API-CALL] POST https://{edr_console}/devices/entities/devices-actions/v2?action_name=contain")
    print("    [API-RESPONSE] 202 Accepted - Isolation command sent to agent client.")
    return True

def main():
    parser = argparse.ArgumentParser(description="SOC SOAR Containment Engine")
    parser.add_argument("--fw-host", default="firewall.corporate.local", help="Firewall Gateway IP/DNS")
    parser.add_argument("--edr-host", default="edr-console.corporate.local", help="EDR SaaS endpoint")
    parser.add_argument("--malicious-ip", required=True, help="IP address to block on Firewall")
    parser.add_argument("--target-host", required=True, help="Hostname of compromised machine to isolate")
    args = parser.parse_args()

    results = {
        "playbook": "SOC_Threat_Containment",
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "firewall_block": "failed",
        "edr_isolation": "failed",
        "status": "partial"
    }

    try:
        # 1. Block malicious IP
        fw_success = block_ip_on_firewall(args.fw_host, "secret-fw-token", args.malicious_ip)
        if fw_success:
            results["firewall_block"] = "success"

        # 2. Isolate compromised workstation
        edr_success = isolate_endpoint_edr(args.edr_host, "secret-edr-token", args.target_host)
        if edr_success:
            results["edr_isolation"] = "success"

        if fw_success and edr_success:
            results["status"] = "success"
            print(json.dumps(results, indent=2))
            print("[SUCCESS] SOC Containment complete. Malicious IP blocked and host isolated.")
            sys.exit(0)
        else:
            results["status"] = "failed"
            print(json.dumps(results, indent=2))
            print("[ERROR] SOAR Containment playbook failed or executed partially.")
            sys.exit(1)
            
    except Exception as e:
        results["status"] = "error"
        results["error_message"] = str(e)
        print(json.dumps(results, indent=2))
        sys.exit(2)

if __name__ == "__main__":
    main()
