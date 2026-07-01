#!/usr/bin/env python3
"""
NOC Application Self-Healing Script - IIS Pool Recycling & Service Restart.
Simulates PowerShell WinRM graceful degradation mitigations.
"""

import sys
import argparse
import time

def restart_service(host, service_name):
    print(f"[*] Connecting via WinRM to {host}...")
    time.sleep(1)
    print(f"[*] Querying status of service '{service_name}'...")
    time.sleep(0.5)
    print(f"[!] Service '{service_name}' is in STOPPED or HUNG state.")
    print(f"[*] Triggering: Restart-Service -Name {service_name} -Force")
    time.sleep(1.5)
    print(f"[+] Service '{service_name}' restarted successfully.")

def recycle_iis_pool(host, pool_name):
    print(f"[*] Connecting via WinRM to IIS Host: {host}...")
    time.sleep(1.2)
    print(f"[*] Executing IIS AppPool recycling for: '{pool_name}'...")
    print("    [PS] $pool = Get-IISAppPool -Name '{0}'".format(pool_name))
    print("    [PS] $pool.Recycle()")
    time.sleep(1.5)
    print(f"[+] Application Pool '{pool_name}' recycled gracefully. Draining active sessions...")
    time.sleep(1)
    print("[+] Session draining complete.")

def main():
    parser = argparse.ArgumentParser(description="NOC Application & Service Self-Healing Playbook")
    parser.add_argument("--host", required=True, help="Target host/IP")
    parser.add_argument("--action", default="iis_recycle", choices=["iis_recycle", "service_restart"], help="Action type")
    parser.add_argument("--target", default="MSExchangeIS", help="Target Pool or Service name")
    args = parser.parse_args()

    try:
        if args.action == "iis_recycle":
            recycle_iis_pool(args.host, args.target)
        else:
            restart_service(args.host, args.target)
            
        print("[SUCCESS] NOC Application Self-Healing complete. Health status restored.")
        sys.exit(0)
    except Exception as e:
        print(f"[FATAL] Application Self-Healing failed: {e}")
        sys.exit(2)

if __name__ == "__main__":
    main()
