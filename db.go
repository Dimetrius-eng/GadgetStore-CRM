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
	const iterations = 120000
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
		`UPDATE sales s SET product_title = p.title FROM products p WHERE s.product_id = p.id AND s.product_title = ''`,
		`ALTER TABLE sales DROP CONSTRAINT IF EXISTS sales_product_id_fkey`,
		`ALTER TABLE sales ADD CONSTRAINT sales_product_id_fkey FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE SET NULL`,
		`CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY, username VARCHAR(50) UNIQUE NOT NULL, password_hash VARCHAR(255) NOT NULL,
			full_name VARCHAR(100) NOT NULL DEFAULT 'Адміністратор', role VARCHAR(20) NOT NULL DEFAULT 'admin',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check`,
		`ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('admin','manager'))`,
	}
	for _, statement := range statements {
		if _, err := DB.Exec(statement); err != nil {
			log.Fatalf("database migration failed: %v", err)
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
	_, err := DB.Exec(`INSERT INTO users (username, password_hash, full_name, role) VALUES ($1,$2,$3,$4)
		ON CONFLICT (username) DO NOTHING`, username, hashPassword(password), fullName, role)
	if err != nil {
		log.Fatalf("seed user %s: %v", username, err)
	}
}
