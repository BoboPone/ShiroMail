package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

type Metrics struct {
	mu              sync.RWMutex
	totalRequests   atomic.Int64
	totalErrors     atomic.Int64
	activeRequests  atomic.Int64
	statusCounts    map[int]*atomic.Int64
	methodCounts    map[string]*atomic.Int64
	smtpRejects     map[string]*atomic.Int64
	smtpAccepted    map[string]*atomic.Int64
	spoolProcessed  map[string]*atomic.Int64
	smtpSessions    atomic.Int64
	smtpRecipientOK atomic.Int64
	smtpBytes       atomic.Int64
	totalLatencyMs  atomic.Int64
	startTime       time.Time
}

type SMTPMetricsSnapshot struct {
	SessionsStarted    int64            `json:"sessionsStarted"`
	RecipientsAccepted int64            `json:"recipientsAccepted"`
	BytesReceived      int64            `json:"bytesReceived"`
	Accepted           map[string]int64 `json:"accepted"`
	Rejected           map[string]int64 `json:"rejected"`
	SpoolProcessed     map[string]int64 `json:"spoolProcessed"`
}

var globalMetrics = &Metrics{
	statusCounts:   make(map[int]*atomic.Int64),
	methodCounts:   make(map[string]*atomic.Int64),
	smtpRejects:    make(map[string]*atomic.Int64),
	smtpAccepted:   make(map[string]*atomic.Int64),
	spoolProcessed: make(map[string]*atomic.Int64),
	startTime:      time.Now(),
}

func RecordSMTPSessionStarted() {
	globalMetrics.smtpSessions.Add(1)
}

func RecordSMTPRecipientAccepted() {
	globalMetrics.smtpRecipientOK.Add(1)
}

func RecordSMTPDeliveryBytes(size int) {
	if size > 0 {
		globalMetrics.smtpBytes.Add(int64(size))
	}
}

func RecordSMTPDeliveryAccepted(mode string) {
	globalMetrics.getSMTPAcceptedCounter(mode).Add(1)
}

func RecordSMTPDeliveryRejected(reason string) {
	globalMetrics.getSMTPRejectCounter(reason).Add(1)
}

func RecordInboundSpoolProcessed(status string) {
	globalMetrics.getSpoolProcessedCounter(status).Add(1)
}

func SnapshotSMTPMetrics() SMTPMetricsSnapshot {
	m := globalMetrics
	snapshot := SMTPMetricsSnapshot{
		SessionsStarted:    m.smtpSessions.Load(),
		RecipientsAccepted: m.smtpRecipientOK.Load(),
		BytesReceived:      m.smtpBytes.Load(),
		Accepted:           map[string]int64{},
		Rejected:           map[string]int64{},
		SpoolProcessed:     map[string]int64{},
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	for key, counter := range m.smtpAccepted {
		snapshot.Accepted[key] = counter.Load()
	}
	for key, counter := range m.smtpRejects {
		snapshot.Rejected[key] = counter.Load()
	}
	for key, counter := range m.spoolProcessed {
		snapshot.SpoolProcessed[key] = counter.Load()
	}
	return snapshot
}

func MetricsMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		globalMetrics.activeRequests.Add(1)
		start := time.Now()

		ctx.Next()

		elapsed := time.Since(start).Milliseconds()
		globalMetrics.totalLatencyMs.Add(elapsed)
		globalMetrics.activeRequests.Add(-1)
		globalMetrics.totalRequests.Add(1)

		status := ctx.Writer.Status()
		if status >= 500 {
			globalMetrics.totalErrors.Add(1)
		}

		globalMetrics.getStatusCounter(status).Add(1)
		globalMetrics.getMethodCounter(ctx.Request.Method).Add(1)
	}
}

func MetricsHandler(token string) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if token != "" {
			provided := bearerToken(ctx)
			if provided != token {
				ctx.AbortWithStatus(http.StatusUnauthorized)
				return
			}
		}
		m := globalMetrics
		uptime := time.Since(m.startTime).Seconds()
		total := m.totalRequests.Load()
		errCount := m.totalErrors.Load()
		active := m.activeRequests.Load()
		latency := m.totalLatencyMs.Load()

		var avgLatency float64
		if total > 0 {
			avgLatency = float64(latency) / float64(total)
		}

		body := fmt.Sprintf(
			"# HELP shiro_http_requests_total Total HTTP requests\n"+
				"# TYPE shiro_http_requests_total counter\n"+
				"shiro_http_requests_total %d\n"+
				"# HELP shiro_http_errors_total Total HTTP 5xx errors\n"+
				"# TYPE shiro_http_errors_total counter\n"+
				"shiro_http_errors_total %d\n"+
				"# HELP shiro_http_active_requests Active HTTP requests\n"+
				"# TYPE shiro_http_active_requests gauge\n"+
				"shiro_http_active_requests %d\n"+
				"# HELP shiro_http_avg_latency_ms Average request latency ms\n"+
				"# TYPE shiro_http_avg_latency_ms gauge\n"+
				"shiro_http_avg_latency_ms %.2f\n"+
				"# HELP shiro_uptime_seconds Server uptime seconds\n"+
				"# TYPE shiro_uptime_seconds gauge\n"+
				"shiro_uptime_seconds %.0f\n",
			total, errCount, active, avgLatency, uptime,
		)

		m.mu.RLock()
		for status, counter := range m.statusCounts {
			body += fmt.Sprintf("shiro_http_status{code=\"%d\"} %d\n", status, counter.Load())
		}
		for method, counter := range m.methodCounts {
			body += fmt.Sprintf("shiro_http_method{method=\"%s\"} %d\n", method, counter.Load())
		}
		for reason, counter := range m.smtpRejects {
			body += fmt.Sprintf("shiro_smtp_messages_rejected_total{reason=\"%s\"} %d\n", reason, counter.Load())
		}
		for mode, counter := range m.smtpAccepted {
			body += fmt.Sprintf("shiro_smtp_messages_accepted_total{mode=\"%s\"} %d\n", mode, counter.Load())
		}
		for status, counter := range m.spoolProcessed {
			body += fmt.Sprintf("shiro_inbound_spool_processed_total{status=\"%s\"} %d\n", status, counter.Load())
		}
		m.mu.RUnlock()

		body += fmt.Sprintf(
			"# HELP shiro_smtp_sessions_total Total SMTP sessions started\n"+
				"# TYPE shiro_smtp_sessions_total counter\n"+
				"shiro_smtp_sessions_total %d\n"+
				"# HELP shiro_smtp_recipients_accepted_total Total SMTP recipients accepted\n"+
				"# TYPE shiro_smtp_recipients_accepted_total counter\n"+
				"shiro_smtp_recipients_accepted_total %d\n"+
				"# HELP shiro_smtp_bytes_received_total Total SMTP DATA bytes received\n"+
				"# TYPE shiro_smtp_bytes_received_total counter\n"+
				"shiro_smtp_bytes_received_total %d\n",
			m.smtpSessions.Load(),
			m.smtpRecipientOK.Load(),
			m.smtpBytes.Load(),
		)

		ctx.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(body))
	}
}

func (m *Metrics) getStatusCounter(status int) *atomic.Int64 {
	m.mu.RLock()
	counter, ok := m.statusCounts[status]
	m.mu.RUnlock()
	if ok {
		return counter
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	counter, ok = m.statusCounts[status]
	if !ok {
		counter = &atomic.Int64{}
		m.statusCounts[status] = counter
	}
	return counter
}

func (m *Metrics) getMethodCounter(method string) *atomic.Int64 {
	m.mu.RLock()
	counter, ok := m.methodCounts[method]
	m.mu.RUnlock()
	if ok {
		return counter
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	counter, ok = m.methodCounts[method]
	if !ok {
		counter = &atomic.Int64{}
		m.methodCounts[method] = counter
	}
	return counter
}

func (m *Metrics) getSMTPRejectCounter(reason string) *atomic.Int64 {
	reason = normalizeMetricLabel(reason, "unknown")
	m.mu.RLock()
	counter, ok := m.smtpRejects[reason]
	m.mu.RUnlock()
	if ok {
		return counter
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	counter, ok = m.smtpRejects[reason]
	if !ok {
		counter = &atomic.Int64{}
		m.smtpRejects[reason] = counter
	}
	return counter
}

func (m *Metrics) getSMTPAcceptedCounter(mode string) *atomic.Int64 {
	mode = normalizeMetricLabel(mode, "direct")
	m.mu.RLock()
	counter, ok := m.smtpAccepted[mode]
	m.mu.RUnlock()
	if ok {
		return counter
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	counter, ok = m.smtpAccepted[mode]
	if !ok {
		counter = &atomic.Int64{}
		m.smtpAccepted[mode] = counter
	}
	return counter
}

func (m *Metrics) getSpoolProcessedCounter(status string) *atomic.Int64 {
	status = normalizeMetricLabel(status, "unknown")
	m.mu.RLock()
	counter, ok := m.spoolProcessed[status]
	m.mu.RUnlock()
	if ok {
		return counter
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	counter, ok = m.spoolProcessed[status]
	if !ok {
		counter = &atomic.Int64{}
		m.spoolProcessed[status] = counter
	}
	return counter
}

func normalizeMetricLabel(value string, fallback string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return fallback
	}
	return value
}

func resetMetricsForTest() {
	globalMetrics = &Metrics{
		statusCounts:   make(map[int]*atomic.Int64),
		methodCounts:   make(map[string]*atomic.Int64),
		smtpRejects:    make(map[string]*atomic.Int64),
		smtpAccepted:   make(map[string]*atomic.Int64),
		spoolProcessed: make(map[string]*atomic.Int64),
		startTime:      time.Now(),
	}
}
