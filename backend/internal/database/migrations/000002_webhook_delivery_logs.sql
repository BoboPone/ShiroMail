CREATE TABLE IF NOT EXISTS webhook_delivery_logs (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  webhook_id BIGINT NOT NULL,
  user_id BIGINT NOT NULL,
  event VARCHAR(64) NOT NULL,
  target_url VARCHAR(1024) NOT NULL,
  request_body JSON NULL,
  response_status INT NULL,
  response_body TEXT NULL,
  latency_ms INT NOT NULL DEFAULT 0,
  success BOOLEAN NOT NULL DEFAULT FALSE,
  error_message VARCHAR(512) NULL,
  attempt INT NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_webhook_delivery_logs_webhook ON webhook_delivery_logs (webhook_id, created_at);
CREATE INDEX idx_webhook_delivery_logs_user ON webhook_delivery_logs (user_id, created_at);

-- Performance index for CountToday query on messages table
CREATE INDEX idx_messages_deleted_received ON messages (is_deleted, received_at);
