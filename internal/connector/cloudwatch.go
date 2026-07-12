package connector

import (
	"encoding/json"
	"strings"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

// CloudWatchAlarmMessage is the CloudWatch alarm state-change JSON — this is the *inner*
// message body already unwrapped from its SNS envelope. This connector never sees the SNS
// envelope itself (Type/SubscribeURL/Message-as-string) — that's handled by
// internal/api/cloudwatch_handler.go, which extracts this JSON and passes it straight through,
// keeping MapToUnified a pure function like every other connector.
type CloudWatchAlarmMessage struct {
	AlarmName        string `json:"AlarmName"`
	AlarmDescription string `json:"AlarmDescription"`
	AWSAccountID     string `json:"AWSAccountId"`
	NewStateValue    string `json:"NewStateValue"` // ALARM, OK, INSUFFICIENT_DATA
	OldStateValue    string `json:"OldStateValue"`
	NewStateReason   string `json:"NewStateReason"`
	StateChangeTime  string `json:"StateChangeTime"` // RFC3339
	Region           string `json:"Region"`
	AlarmArn         string `json:"AlarmArn"`
	Trigger          struct {
		MetricName string `json:"MetricName"`
		Namespace  string `json:"Namespace"`
		Dimensions []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"Dimensions"`
	} `json:"Trigger"`
}

type cloudWatchConnector struct{}

func init() {
	Register(cloudWatchConnector{})
}

func (cloudWatchConnector) Type() model.IncidentSource { return model.SourceCloudWatch }

func (cloudWatchConnector) MapToUnified(rawPayload []byte, tenantID uuid.UUID) ([]model.UnifiedIncident, error) {
	var msg CloudWatchAlarmMessage
	if err := json.Unmarshal(rawPayload, &msg); err != nil {
		return nil, err
	}

	severity := model.SeverityInfo
	switch msg.NewStateValue {
	case "ALARM":
		severity = model.SeverityCritical
	case "INSUFFICIENT_DATA":
		severity = model.SeverityWarning
	case "OK":
		severity = model.SeverityInfo
	}

	host := msg.Region
	for _, dim := range msg.Trigger.Dimensions {
		if dim.Name == "InstanceId" || strings.EqualFold(dim.Name, "host") {
			host = dim.Value
			break
		}
	}

	timestamp, err := time.Parse(time.RFC3339, msg.StateChangeTime)
	if err != nil {
		timestamp = time.Now()
	}

	rawMap := make(map[string]interface{})
	rawMap["old_state"] = msg.OldStateValue
	rawMap["new_state"] = msg.NewStateValue
	rawMap["metric_name"] = msg.Trigger.MetricName
	rawMap["namespace"] = msg.Trigger.Namespace
	rawMap["aws_account_id"] = msg.AWSAccountID

	incident := model.UnifiedIncident{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Source:      model.SourceCloudWatch,
		ExternalID:  msg.AlarmArn,
		EventType:   "cloudwatch_alarm",
		Severity:    severity,
		Title:       msg.AlarmName,
		Description: msg.NewStateReason,
		Host:        host,
		RawPayload:  rawMap,
		Timestamp:   timestamp,
	}
	return []model.UnifiedIncident{incident}, nil
}
