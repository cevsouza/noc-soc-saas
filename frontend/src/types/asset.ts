// CMDB assets (topology slice T2). Mirrors internal/api/assets.go. AssetView is one row of the merged
// CMDB list: the managed overlay (business criticality, owner, location, tags, notes) plus the
// discovery facts. `managed` = has a manual overlay row; `discovered` = responded to the SNMP sweep.
export type BusinessCriticality = 'low' | 'medium' | 'high' | 'critical';

export interface AssetView {
  identifier: string;
  name: string;
  asset_type: string;
  vendor: string;
  business_criticality: BusinessCriticality;
  owner: string;
  location: string;
  tags: string[];
  notes: string;
  managed: boolean;
  discovered: boolean;
  sysname?: string;
  last_seen?: string;
}

// AssetInput is the POST body to upsert an asset annotation.
export interface AssetInput {
  identifier: string;
  name: string;
  asset_type?: string;
  vendor?: string;
  business_criticality?: BusinessCriticality;
  owner?: string;
  location?: string;
  tags?: string[];
  notes?: string;
}
