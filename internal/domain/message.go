package domain

type WebhookPayload struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Path      string `json:"path"`
	Event     string `json:"event"`
	Payload   any    `json:"payload"`
	Timestamp int64  `json:"timestamp"` // UnixNano for high precision
}
