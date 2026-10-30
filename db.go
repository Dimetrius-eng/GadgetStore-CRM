package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"

	_ "github.com/lib/pq"
)

var DB *sql.DB

func hashPassword(password string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic("cannot generate password salt")
	}
	const iterations = 600000
	derived := pbkdf2SHA256([]byte(password), salt, iterations)
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iterations, hex.EncodeToString(salt), hex.EncodeToString(derived))
}

func verifyPassword(password, encoded string) bool {
	fields := strings.Split(encoded, "$")
	if len(fields) == 4 && fields[0] == "pbkdf2-sha256" {
		iterations, err := strconv.Atoi(fields[1])
		salt, saltErr := hex.DecodeString(fields[2])
		expected, hashErr := hex.DecodeString(fields[3])
		if err != nil || saltErr != nil || hashErr != nil || iterations < 10000 || iterations > 1000000 {
			return false
		}
		actual := pbkdf2SHA256([]byte(password), salt, iterations)
		return len(actual) == len(expected) && subtle.ConstantTimeCompare(actual, expected) == 1
	}
	legacy := sha256.Sum256([]byte(password))
	legacyHex := hex.EncodeToString(legacy[:])
	return len(encoded) == len(legacyHex) && subtle.ConstantTimeCompare([]byte(encoded), []byte(legacyHex)) == 1
}

func passwordHashNeedsUpgrade(encoded string) bool {
	fields := strings.SplitN(encoded, "$", 3)
	if len(fields) != 3 || fields[0] != "pbkdf2-sha256" {
		return true
	}
	iterations, err := strconv.Atoi(fields[1])
	return err != nil || iterations < 600000
}

func pbkdf2SHA256(password, salt []byte, iterations int) []byte {
	mac := hmac.New(sha256.New, password)
	mac.Write(salt)
	mac.Write([]byte{0, 0, 0, 1})
	u := mac.Sum(nil)
	result := append([]byte(nil), u...)
	for i := 1; i < iterations; i++ {
		mac.Reset()
		mac.Write(u)
		u = mac.Sum(nil)
		for j := range result {
			result[j] ^= u[j]
		}
	}
	return result
}

func InitDB() {
	conn := os.Getenv("DATABASE_URL")
	if envOr("APP_ENV", "") == "production" {
		if conn == "" {
			log.Fatal("DATABASE_URL must be configured in production")
		}
		parsed, err := url.Parse(conn)
		if err != nil || parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
			log.Fatal("DATABASE_URL must be a valid PostgreSQL URL")
		}
		sslmode := parsed.Query().Get("sslmode")
		if sslmode != "require" && sslmode != "verify-ca" && sslmode != "verify-full" {
			log.Fatal("DATABASE_URL must require TLS with sslmode=require or stronger")
		}
	}
	if conn == "" {
		port := envOr("DB_PORT", "5433")
		user := envOr("DB_USER", "postgres")
		password := envOr("DB_PASSWORD", "admin123")
		database := envOr("DB_NAME", "gadget_store_db")
		conn = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable connect_timeout=5",
			envOr("DB_HOST", "127.0.0.1"), port, user, password, database)
	}
	var err error
	DB, err = sql.Open("postgres", conn)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	if err = DB.Ping(); err != nil {
		log.Fatalf("connect to PostgreSQL: %v", err)
	}

	statements := []string{
		`CREATE TABLE IF NOT EXISTS categories (id SERIAL PRIMARY KEY, name VARCHAR(100) NOT NULL UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS brands (id SERIAL PRIMARY KEY, name VARCHAR(100) NOT NULL UNIQUE)`,
		`CREATE TABLE IF NOT EXISTS products (
			id SERIAL PRIMARY KEY, title VARCHAR(255) NOT NULL, sku VARCHAR(100) UNIQUE NOT NULL,
			category_id INT REFERENCES categories(id) ON DELETE SET NULL,
			brand_id INT REFERENCES brands(id) ON DELETE SET NULL,
			purchase_price NUMERIC(10,2) NOT NULL DEFAULT 0, selling_price NUMERIC(10,2) NOT NULL DEFAULT 0,
			quantity INT NOT NULL DEFAULT 0, warranty_months INT NOT NULL DEFAULT 0,
			image_path VARCHAR(500) NOT NULL DEFAULT '', created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`ALTER TABLE products ADD COLUMN IF NOT EXISTS image_path VARCHAR(500) NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS sales (
			id SERIAL PRIMARY KEY, product_id INT REFERENCES products(id) ON DELETE SET NULL,
			product_title VARCHAR(255) NOT NULL DEFAULT '',
			quantity_sold INT NOT NULL, unit_price NUMERIC(10,2) NOT NULL,
			total_price NUMERIC(10,2) NOT NULL, profit NUMERIC(10,2) NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`ALTER TABLE sales ALTER COLUMN product_id DROP NOT NULL`,
		`ALTER TABLE sales ADD COLUMN IF NOT EXISTS product_title VARCHAR(255) NOT NULL DEFAULT ''`,
		`ALTER TABLE sales ADD COLUMN IF NOT EXISTS customer_phone VARCHAR(32) NOT NULL DEFAULT ''`,
		`UPDATE sales s SET product_title = p.title FROM products p WHERE s.product_id = p.id AND s.product_title = ''`,
		`ALTER TABLE sales DROP CONSTRAINT IF EXISTS sales_product_id_fkey`,
		`ALTER TABLE sales ADD CONSTRAINT sales_product_id_fkey FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE SET NULL`,
		`CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY, username VARCHAR(50) UNIQUE NOT NULL, password_hash VARCHAR(255) NOT NULL,
			full_name VARCHAR(100) NOT NULL DEFAULT 'Адміністратор', role VARCHAR(20) NOT NULL DEFAULT 'admin',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS auth_sessions (
			token_hash CHAR(64) PRIMARY KEY,
			user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL,
			last_activity TIMESTAMPTZ NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS auth_sessions_expiry_idx ON auth_sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS auth_sessions_activity_idx ON auth_sessions(last_activity)`,
		`ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check`,
		`ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('admin','manager'))`,
		`CREATE TABLE IF NOT EXISTS demo_sessions (
			token CHAR(64) PRIMARY KEY,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			expires_at TIMESTAMPTZ NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS demo_sessions_expires_at_idx ON demo_sessions(expires_at)`,
		`CREATE TABLE IF NOT EXISTS demo_products (
			id BIGSERIAL PRIMARY KEY,
			session_token CHAR(64) NOT NULL REFERENCES demo_sessions(token) ON DELETE CASCADE,
			title VARCHAR(255) NOT NULL,
			sku VARCHAR(100) NOT NULL,
			brand VARCHAR(100) NOT NULL,
			category VARCHAR(100) NOT NULL,
			purchase_price NUMERIC(10,2) NOT NULL DEFAULT 0,
			selling_price NUMERIC(10,2) NOT NULL DEFAULT 0,
			warranty_months INT NOT NULL DEFAULT 12,
			quantity INT NOT NULL DEFAULT 0,
			image_data BYTEA,
			image_content_type VARCHAR(50),
			UNIQUE(session_token, sku))`,
		`ALTER TABLE demo_products ADD COLUMN IF NOT EXISTS warranty_months INT NOT NULL DEFAULT 12`,
		`ALTER TABLE demo_products ADD COLUMN IF NOT EXISTS image_data BYTEA`,
		`ALTER TABLE demo_products ADD COLUMN IF NOT EXISTS image_content_type VARCHAR(50)`,
		`CREATE TABLE IF NOT EXISTS demo_categories (
			id BIGSERIAL PRIMARY KEY, session_token CHAR(64) NOT NULL REFERENCES demo_sessions(token) ON DELETE CASCADE,
			name VARCHAR(100) NOT NULL, UNIQUE(session_token,name))`,
		`CREATE TABLE IF NOT EXISTS demo_brands (
			id BIGSERIAL PRIMARY KEY, session_token CHAR(64) NOT NULL REFERENCES demo_sessions(token) ON DELETE CASCADE,
			name VARCHAR(100) NOT NULL, UNIQUE(session_token,name))`,
		`CREATE TABLE IF NOT EXISTS demo_users (
			id BIGSERIAL PRIMARY KEY, session_token CHAR(64) NOT NULL REFERENCES demo_sessions(token) ON DELETE CASCADE,
			full_name VARCHAR(100) NOT NULL, username VARCHAR(50) NOT NULL, role VARCHAR(20) NOT NULL DEFAULT 'manager',
			UNIQUE(session_token,username))`,
		`CREATE TABLE IF NOT EXISTS demo_customers (
			id BIGSERIAL PRIMARY KEY,
			session_token CHAR(64) NOT NULL REFERENCES demo_sessions(token) ON DELETE CASCADE,
			name VARCHAR(160) NOT NULL,
			phone VARCHAR(32) NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS demo_sales (
			id BIGSERIAL PRIMARY KEY,
			session_token CHAR(64) NOT NULL REFERENCES demo_sessions(token) ON DELETE CASCADE,
			product_title VARCHAR(255) NOT NULL,
			customer_name VARCHAR(160) NOT NULL,
			quantity INT NOT NULL,
			unit_price NUMERIC(10,2) NOT NULL,
			total_price NUMERIC(10,2) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE INDEX IF NOT EXISTS demo_products_session_idx ON demo_products(session_token)`,
		`CREATE INDEX IF NOT EXISTS demo_categories_session_idx ON demo_categories(session_token)`,
		`CREATE INDEX IF NOT EXISTS demo_brands_session_idx ON demo_brands(session_token)`,
		`CREATE INDEX IF NOT EXISTS demo_users_session_idx ON demo_users(session_token)`,
		`CREATE INDEX IF NOT EXISTS demo_customers_session_idx ON demo_customers(session_token)`,
		`CREATE INDEX IF NOT EXISTS demo_sales_session_idx ON demo_sales(session_token)`,
	}
	for _, statement := range statements {
		if _, err := DB.Exec(statement); err != nil {
			log.Fatalf("database migration failed: %v", err)
		}
	}
	if strings.TrimSpace(os.Getenv("SUPABASE_URL")) != "" {
		// The CRM uses PostgreSQL directly from Go and does not use the Supabase
		// Data API for application tables. Remove its public roles' access so a
		// discovered project URL/key cannot expose business data through REST.
		for _, role := range []string{"anon", "authenticated"} {
			var roleExists bool
			if err := DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, role).Scan(&roleExists); err != nil {
				log.Fatalf("could not inspect Supabase API roles: %v", err)
			}
			if !roleExists {
				continue
			}
			statements := []string{
				"REVOKE ALL ON SCHEMA public FROM " + role,
				"REVOKE ALL ON ALL TABLES IN SCHEMA public FROM " + role,
				"REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM " + role,
				"REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA public FROM " + role,
				"ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM " + role,
				"ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON SEQUENCES FROM " + role,
				"ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE EXECUTE ON FUNCTIONS FROM " + role,
			}
			for _, statement := range statements {
				if _, err := DB.Exec(statement); err != nil {
					log.Fatalf("could not restrict Supabase API role %s: %v", role, err)
				}
			}
		}
	}
	adminUsername := envOr("ADMIN_USERNAME", "admin")
	adminPassword := envOr("ADMIN_PASSWORD", "admin123")
	managerUsername := envOr("MANAGER_USERNAME", "manager")
	managerPassword := envOr("MANAGER_PASSWORD", "manager123")
	if envOr("APP_ENV", "") == "production" {
		if len(adminPassword) < 12 || adminPassword == "admin123" {
			log.Fatal("ADMIN_PASSWORD must be configured with at least 12 characters in production")
		}
		if len(managerPassword) < 12 || managerPassword == "manager123" {
			log.Fatal("MANAGER_PASSWORD must be configured with at least 12 characters in production")
		}
	}
	seedUser(adminUsername, adminPassword, "Адміністратор", "admin")
	seedUser(managerUsername, managerPassword, "Менеджер магазину", "manager")
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func seedUser(username, password, fullName, role string) {
	var exists bool
	if err := DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE LOWER(username)=LOWER($1))`, username).Scan(&exists); err != nil {
		log.Fatalf("check seed user %s: %v", username, err)
	}
	if exists {
		return
	}
	_, err := DB.Exec(`INSERT INTO users (username, password_hash, full_name, role) VALUES ($1,$2,$3,$4)
		ON CONFLICT (username) DO NOTHING`, username, hashPassword(password), fullName, role)
	if err != nil {
		log.Fatalf("seed user %s: %v", username, err)
	}
}
