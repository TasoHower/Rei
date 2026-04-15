package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const createUserFactsTable = `
CREATE TABLE IF NOT EXISTS user_facts (
    id         BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id    VARCHAR(128) NOT NULL,
    category   VARCHAR(64)  NOT NULL,
    fact_key   VARCHAR(256) NOT NULL,
    fact_value TEXT         NOT NULL,
    confidence FLOAT        DEFAULT 0.8,
    source_conversation_id VARCHAR(128),
    created_at TIMESTAMP    DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP    DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_user_id (user_id),
    INDEX idx_user_category (user_id, category),
    UNIQUE KEY uk_user_fact (user_id, category, fact_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

func NewMySQL(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	if _, err := db.ExecContext(ctx, createUserFactsTable); err != nil {
		db.Close()
		return nil, fmt.Errorf("auto-migrate user_facts: %w", err)
	}

	return db, nil
}
