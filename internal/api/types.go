package api

import "github.com/libi/libi-print-agent/internal/spool"

// PairRequest is the body of POST /print-agents/pair.
type PairRequest struct {
	Code     string              `json:"code"`
	Name     string              `json:"name,omitempty"`
	Hostname string              `json:"hostname"`
	Version  string              `json:"version"`
	Printers []spool.PrinterInfo `json:"printers"`
}

// PairResult is the 201 body of POST /print-agents/pair.
type PairResult struct {
	Token      string `json:"token"`
	AgentID    string `json:"agentId"`
	MerchantID string `json:"merchantId"`
	Name       string `json:"name"`
}

// HeartbeatRequest is the body of POST /print-agents/heartbeat.
type HeartbeatRequest struct {
	Printers []spool.PrinterInfo `json:"printers"`
	Version  string              `json:"version"`
}

// Job is one entry of GET /print-agents/jobs/pending, oldest first.
type Job struct {
	ID            string `json:"id"`
	Seq           int64  `json:"seq"`
	TicketType    string `json:"ticketType"`
	PrinterName   string `json:"printerName"`
	OrderNumber   *int64 `json:"orderNumber"`
	PayloadBase64 string `json:"payloadBase64"`
	CreatedAt     string `json:"createdAt"`
}

type pendingJobsResponse struct {
	Jobs []Job `json:"jobs"`
}

// JobOutcome is the ack status the agent reports for a job.
type JobOutcome string

const (
	JobPrinted JobOutcome = "printed"
	JobFailed  JobOutcome = "failed"
)

type ackRequest struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// VersionInfo is the body of GET /print-agents/version. Version is a
// pointer because the endpoint reports {"version":null} when no release
// has been published yet, which callers must tell apart from an empty
// string.
type VersionInfo struct {
	Version *string `json:"version"`
	URL     string  `json:"url,omitempty"`
	SHA256  string  `json:"sha256,omitempty"`
}
