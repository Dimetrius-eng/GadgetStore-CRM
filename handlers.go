package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Проста система сесій у пам'яті
var (
	userSessions = make(map[string]User) // session_token -> authenticated user
	sessionsLock sync.Mutex
)

// Форматування грошових сум (наприклад, 1137459.50 -> "1 137 459.50 ₴")
func formatMoney(val float64) string {
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return "—"
	}
	// Round to cents before splitting the amount: binary floating point can
	// otherwise turn values such as 1.15 into 1.14.
	centsFloat := math.Round(val*100 + math.Copysign(1e-9, val))
	if centsFloat >= float64(math.MaxInt64) || centsFloat <= float64(math.MinInt64) {
		return "—"
	}
	cents := int64(centsFloat)
	negative := cents < 0
	if negative {
		cents = -cents
	}
	in, frac := cents/100, cents%100
	str := strconv.FormatInt(in, 10)
	var result []byte
	l := len(str)
	for i, c := range str {
		if i > 0 && (l-i)%3 == 0 && c != '-' {
			result = append(result, ' ')
		}
		result = append(result, byte(c))
	}
	if negative {
		result = append([]byte{'-'}, result...)
	}
	return fmt.Sprintf("%s.%02d ₴", string(result), frac)
}

func validSaleQuantity(sold, available int) bool {
	return sold > 0 && available >= sold
}

func normalizeCustomerPhone(phone string) (string, error) {
	var digits strings.Builder
	for _, char := range strings.TrimSpace(phone) {
		switch {
		case char >= '0' && char <= '9':
			digits.WriteRune(char)
		case char == '+' || char == '-' || char == '(' || char == ')' || char == '.' || char == ' ':
			// Ignore common formatting characters.
		default:
			return "", fmt.Errorf("Введіть номер телефону цифрами та символами +, -, дужками або пробілами")
		}
	}
	normalized := digits.String()
	if normalized == "" {
		return "", nil
	}
	if len(normalized) < 7 || len(normalized) > 15 {
		return "", fmt.Errorf("Номер телефону має містити від 7 до 15 цифр")
	}
	return normalized, nil
}

var funcMap = template.FuncMap{
	"formatMoney": formatMoney,
}

// Допоміжна функція рендерингу з кастомними функціями форматування
func renderTemplate(w http.ResponseWriter, pageName string, data interface{}) {
	files := []string{"templates/layout.html", "templates/" + pageName}

	tmpl, err := template.New("layout.html").Funcs(funcMap).ParseFiles(files...)
	if err != nil {
		tmpl, err = template.New("layout").Funcs(funcMap).ParseFiles(files...)
	}

	if err != nil {
		log.Printf("❌ Помилка зчитування шаблонів (%s): %v", pageName, err)
		http.Error(w, "Помилка шаблонізатора: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = tmpl.ExecuteTemplate(w, "layout", data)
	if err != nil {
		err = tmpl.Execute(w, data)
	}

	if err != nil {
		log.Printf("❌ Помилка рендерингу сторінки (%s): %v", pageName, err)
		http.Error(w, "Помилка рендерингу: "+err.Error(), http.StatusInternalServerError)
	}
}

// Структури даних
type User struct {
	ID       int
	Username string
	FullName string
	Role     string
}

type Product struct {
	ID            int
	Title         string
	SKU           string
	CategoryID    int
	CategoryName  string
	BrandID       int
	BrandName     string
	PurchasePrice float64
	SellingPrice  float64
	Quantity      int
	WarrantyMonth int
	ImagePath     string
}

type Category struct {
	ID           int
	Name         string
	ProductCount int
}

type Brand struct {
	ID           int
	Name         string
	ProductCount int
}

type Sale struct {
	ID                 int
	ProductID          int
	ProductTitle       string
	CustomerPhone      string
	QuantitySold       int
	UnitPrice          float64
	TotalPrice         float64
	Profit             float64
	CreatedAtFormatted string
	CreatedAtISO       string
}

type PageContext struct {
	CurrentUser string
	Role        string
	Data        interface{}
}

type ProductsPageData struct {
	Products    []Product
	SearchQuery string
}

type FormPageData struct {
	Product    Product
	Categories []Category
	Brands     []Brand
}

type UsersPageData struct{ Users []User }

type SalesPageData struct {
	Products []Product
	Sales    []Sale
}

type TopSaleItem struct {
	Title        string
	TotalSold    int
	TotalRevenue float64
}

type DashboardData struct {
	TotalProducts     int
	TotalInventoryVal float64
	TotalRevenue      float64
	TotalProfit       float64
	LowStockProducts  []Product
	TopSales          []TopSaleItem
}

func currentUser(r *http.Request) User {
	if cookie, err := r.Cookie("session_token"); err == nil {
		sessionsLock.Lock()
		defer sessionsLock.Unlock()
		return userSessions[cookie.Value]
	}
	return User{}
}

// Middleware перевірки авторизації
func AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_token")
		if err != nil || cookie.Value == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		sessionsLock.Lock()
		_, exists := userSessions[cookie.Value]
		sessionsLock.Unlock()

		if !exists {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next(w, r)
	}
}

func getCurrentUsername(r *http.Request) string {
	cookie, err := r.Cookie("session_token")
	if err == nil && cookie.Value != "" {
		sessionsLock.Lock()
		defer sessionsLock.Unlock()
		return userSessions[cookie.Value].FullName
	}
	return "Адміністратор"
}

// Вхід у систему
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		username := strings.TrimSpace(r.FormValue("username"))
		password := strings.TrimSpace(r.FormValue("password"))

		var user User
		var passwordHash string

		err := DB.QueryRow(`SELECT id, username, password_hash, full_name, role FROM users WHERE LOWER(username) = LOWER($1)`, username).
			Scan(&user.ID, &user.Username, &passwordHash, &user.FullName, &user.Role)

		if err == nil && verifyPassword(password, passwordHash) {
			tokenBytes := make([]byte, 32)
			if _, err := rand.Read(tokenBytes); err != nil {
				http.Error(w, "Не вдалося створити сесію", http.StatusInternalServerError)
				return
			}
			sessionToken := hex.EncodeToString(tokenBytes)

			sessionsLock.Lock()
			userSessions[sessionToken] = user
			sessionsLock.Unlock()

			http.SetCookie(w, &http.Cookie{
				Name:     "session_token",
				Value:    sessionToken,
				Path:     "/",
				HttpOnly: true,
				Secure:   envOr("APP_ENV", "") == "production",
				SameSite: http.SameSiteLaxMode,
				MaxAge:   60 * 60 * 12,
			})

			log.Printf("✅ Успішний вхід користувача: %s", user.Username)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		log.Printf("⚠️ Невдала спроба входу для користувача: '%s'", username)
		tmpl, _ := template.ParseFiles("templates/login.html")
		tmpl.Execute(w, map[string]interface{}{"Error": "Невірний логін або пароль!"})
		return
	}

	tmpl, _ := template.ParseFiles("templates/login.html")
	tmpl.Execute(w, nil)
}

// Вихід із системи
func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_token")
	if err == nil {
		sessionsLock.Lock()
		delete(userSessions, cookie.Value)
		sessionsLock.Unlock()
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   envOr("APP_ENV", "") == "production",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// 1. Головна сторінка (Дашборд) з аналітикою
func IndexHandler(w http.ResponseWriter, r *http.Request) {
	var dash DashboardData

	_ = DB.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(selling_price * quantity), 0) 
		FROM products
	`).Scan(&dash.TotalProducts, &dash.TotalInventoryVal)

	_ = DB.QueryRow(`
		SELECT COALESCE(SUM(total_price), 0), COALESCE(SUM(profit), 0) 
		FROM sales
	`).Scan(&dash.TotalRevenue, &dash.TotalProfit)

	lowRows, err := DB.Query(`
		SELECT id, title, sku, quantity, selling_price 
		FROM products 
		WHERE quantity < 3 
		ORDER BY quantity ASC 
		LIMIT 5
	`)
	if err == nil {
		defer lowRows.Close()
		for lowRows.Next() {
			var p Product
			lowRows.Scan(&p.ID, &p.Title, &p.SKU, &p.Quantity, &p.SellingPrice)
			dash.LowStockProducts = append(dash.LowStockProducts, p)
		}
	}

	topRows, err := DB.Query(`
		SELECT COALESCE(p.title, s.product_title), SUM(s.quantity_sold) as total_sold, SUM(s.total_price) as total_revenue
		FROM sales s
		LEFT JOIN products p ON s.product_id = p.id
		GROUP BY COALESCE(p.title, s.product_title)
		ORDER BY total_sold DESC
		LIMIT 5
	`)
	if err == nil {
		defer topRows.Close()
		for topRows.Next() {
			var item TopSaleItem
			topRows.Scan(&item.Title, &item.TotalSold, &item.TotalRevenue)
			dash.TopSales = append(dash.TopSales, item)
		}
	}

	renderTemplate(w, "index.html", PageContext{
		CurrentUser: getCurrentUsername(r), Role: currentUser(r).Role,
		Data: dash,
	})
}

// 2. Список товарів
func ProductsHandler(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")

	query := `
		SELECT p.id, p.title, p.sku, COALESCE(c.name, 'Без категорії'), COALESCE(b.name, 'Без бренду'), p.purchase_price, p.selling_price, p.quantity, p.warranty_months, p.image_path
		FROM products p
		LEFT JOIN categories c ON p.category_id = c.id
		LEFT JOIN brands b ON p.brand_id = b.id
		WHERE p.title ILIKE $1 OR p.sku ILIKE $1
		ORDER BY p.id DESC
	`

	rows, err := DB.Query(query, "%"+search+"%")
	if err != nil {
		http.Error(w, "Помилка БД: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var p Product
		rows.Scan(&p.ID, &p.Title, &p.SKU, &p.CategoryName, &p.BrandName, &p.PurchasePrice, &p.SellingPrice, &p.Quantity, &p.WarrantyMonth, &p.ImagePath)
		products = append(products, p)
	}

	renderTemplate(w, "products.html", PageContext{
		CurrentUser: getCurrentUsername(r), Role: currentUser(r).Role,
		Data: ProductsPageData{
			Products:    products,
			SearchQuery: search,
		},
	})
}

// 3. Додавання товару
func ProductAddHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		product, err := parseProductForm(w, r, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		_, err = DB.Exec(`
			INSERT INTO products (title, sku, category_id, brand_id, purchase_price, selling_price, quantity, warranty_months, image_path)
			VALUES ($1, $2, NULLIF($3,0), NULLIF($4,0), $5, $6, $7, $8, $9)`,
			product.Title, product.SKU, product.CategoryID, product.BrandID, product.PurchasePrice, product.SellingPrice, product.Quantity, product.WarrantyMonth, product.ImagePath)

		if err != nil {
			http.Error(w, "Помилка збереження в БД: "+err.Error(), http.StatusInternalServerError)
			return
		}

		http.Redirect(w, r, "/products", http.StatusSeeOther)
		return
	}

	renderForm(w, r, Product{})
}

// 4. Редагування товару
func ProductEditHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, _ := strconv.Atoi(idStr)

	if r.Method == http.MethodPost {
		var oldImage string
		if err := DB.QueryRow(`SELECT image_path FROM products WHERE id=$1`, id).Scan(&oldImage); err != nil {
			http.NotFound(w, r)
			return
		}
		product, err := parseProductForm(w, r, oldImage)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		_, err = DB.Exec(`
			UPDATE products 
			SET title=$1, sku=$2, category_id=NULLIF($3,0), brand_id=NULLIF($4,0), purchase_price=$5, selling_price=$6, quantity=$7, warranty_months=$8, image_path=$9
			WHERE id=$10`,
			product.Title, product.SKU, product.CategoryID, product.BrandID, product.PurchasePrice, product.SellingPrice, product.Quantity, product.WarrantyMonth, product.ImagePath, id)

		if err != nil {
			http.Error(w, "Не вдалося оновити гаджет: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if oldImage != product.ImagePath {
			removeProductImage(oldImage)
		}

		http.Redirect(w, r, "/products", http.StatusSeeOther)
		return
	}

	var p Product
	_ = DB.QueryRow(`SELECT id, title, sku, COALESCE(category_id,0), COALESCE(brand_id,0), purchase_price, selling_price, quantity, warranty_months, image_path FROM products WHERE id=$1`, id).
		Scan(&p.ID, &p.Title, &p.SKU, &p.CategoryID, &p.BrandID, &p.PurchasePrice, &p.SellingPrice, &p.Quantity, &p.WarrantyMonth, &p.ImagePath)

	renderForm(w, r, p)
}

// 5. Видалення товару
func ProductDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	idStr := r.URL.Query().Get("id")
	id, _ := strconv.Atoi(idStr)

	var image string
	_ = DB.QueryRow(`SELECT image_path FROM products WHERE id=$1`, id).Scan(&image)
	if _, err := DB.Exec("DELETE FROM products WHERE id=$1", id); err != nil {
		http.Error(w, "Не вдалося видалити товар", http.StatusInternalServerError)
		return
	}
	removeProductImage(image)
	http.Redirect(w, r, "/products", http.StatusSeeOther)
}

// 6. Сторінка Каси
func SalesHandler(w http.ResponseWriter, r *http.Request) {
	prodRows, err := DB.Query(`SELECT id, title, sku, selling_price, quantity, COALESCE(warranty_months, 0) FROM products ORDER BY title`)
	if err != nil {
		http.Error(w, "Не вдалося завантажити товари", http.StatusInternalServerError)
		return
	}
	defer prodRows.Close()

	var products []Product
	for prodRows.Next() {
		var p Product
		if err := prodRows.Scan(&p.ID, &p.Title, &p.SKU, &p.SellingPrice, &p.Quantity, &p.WarrantyMonth); err != nil {
			http.Error(w, "Помилка читання товарів", http.StatusInternalServerError)
			return
		}
		products = append(products, p)
	}

	salesRows, err := DB.Query(`
		SELECT s.id, COALESCE(p.title, s.product_title), s.customer_phone, s.quantity_sold, s.unit_price, s.total_price, s.profit,
			TO_CHAR(s.created_at, 'DD.MM.YYYY HH24:MI'), TO_CHAR(s.created_at, 'YYYY-MM-DD')
		FROM sales s
		LEFT JOIN products p ON s.product_id = p.id
		ORDER BY s.id DESC
	`)
	if err != nil {
		http.Error(w, "Не вдалося завантажити історію продажів", http.StatusInternalServerError)
		return
	}
	defer salesRows.Close()

	var sales []Sale
	for salesRows.Next() {
		var s Sale
		if err := salesRows.Scan(&s.ID, &s.ProductTitle, &s.CustomerPhone, &s.QuantitySold, &s.UnitPrice, &s.TotalPrice, &s.Profit, &s.CreatedAtFormatted, &s.CreatedAtISO); err != nil {
			http.Error(w, "Помилка читання історії продажів", http.StatusInternalServerError)
			return
		}
		sales = append(sales, s)
	}

	renderTemplate(w, "sales.html", PageContext{
		CurrentUser: getCurrentUsername(r), Role: currentUser(r).Role,
		Data: SalesPageData{
			Products: products,
			Sales:    sales,
		},
	})
}

// 7. Створення продажу
func SaleCreateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/sales", http.StatusSeeOther)
		return
	}

	productID, _ := strconv.Atoi(r.FormValue("product_id"))
	customerPhone, phoneErr := normalizeCustomerPhone(r.FormValue("customer_phone"))
	if phoneErr != nil {
		http.Error(w, phoneErr.Error(), http.StatusBadRequest)
		return
	}
	qtySold, qtyErr := strconv.Atoi(r.FormValue("quantity"))
	if qtyErr != nil || qtySold <= 0 {
		http.Error(w, "Кількість має бути більшою за нуль", http.StatusBadRequest)
		return
	}

	var currentQty int
	var purchasePrice, sellingPrice float64
	err := DB.QueryRow(`SELECT quantity, purchase_price, selling_price FROM products WHERE id=$1`, productID).
		Scan(&currentQty, &purchasePrice, &sellingPrice)

	if err != nil || !validSaleQuantity(qtySold, currentQty) {
		http.Error(w, "Недостатньо товару на складі!", http.StatusBadRequest)
		return
	}

	totalPrice := sellingPrice * float64(qtySold)
	profit := (sellingPrice - purchasePrice) * float64(qtySold)

	tx, err := DB.Begin()
	if err != nil {
		http.Error(w, "Помилка транзакції", http.StatusInternalServerError)
		return
	}

	var saleID int
	err = tx.QueryRow(`
		INSERT INTO sales (product_id, product_title, quantity_sold, unit_price, total_price, profit, customer_phone)
		VALUES ($1, (SELECT title FROM products WHERE id=$1), $2, $3, $4, $5, $6) RETURNING id`,
		productID, qtySold, sellingPrice, totalPrice, profit, customerPhone).Scan(&saleID)

	if err != nil {
		tx.Rollback()
		http.Error(w, "Не вдалося зберегти продаж", http.StatusInternalServerError)
		return
	}

	result, err := tx.Exec(`UPDATE products SET quantity = quantity - $1 WHERE id = $2 AND quantity >= $1`, qtySold, productID)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Не вдалося списати товар", http.StatusInternalServerError)
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		tx.Rollback()
		http.Error(w, "Недостатньо товару на складі!", http.StatusBadRequest)
		return
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "Не вдалося завершити продаж", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/sales/receipt?id=%d", saleID), http.StatusSeeOther)
}

// 8. Чек продажу
func SaleReceiptHandler(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, _ := strconv.Atoi(idStr)

	var s Sale
	err := DB.QueryRow(`
		SELECT s.id, COALESCE(p.title, s.product_title), s.quantity_sold, s.unit_price, s.total_price, s.profit, TO_CHAR(s.created_at, 'DD.MM.YYYY HH24:MI')
		FROM sales s
		LEFT JOIN products p ON s.product_id = p.id
		WHERE s.id = $1
	`, id).Scan(&s.ID, &s.ProductTitle, &s.QuantitySold, &s.UnitPrice, &s.TotalPrice, &s.Profit, &s.CreatedAtFormatted)

	if err != nil {
		http.Redirect(w, r, "/sales", http.StatusSeeOther)
		return
	}

	tmpl, _ := template.New("receipt.html").Funcs(funcMap).ParseFiles("templates/receipt.html")
	tmpl.Execute(w, s)
}

// 9. Категорії
func CategoriesHandler(w http.ResponseWriter, r *http.Request) {
	rows, _ := DB.Query(`
		SELECT c.id, c.name, COUNT(p.id) 
		FROM categories c 
		LEFT JOIN products p ON c.id = p.category_id 
		GROUP BY c.id, c.name 
		ORDER BY c.name
	`)
	defer rows.Close()

	var categories []Category
	for rows.Next() {
		var c Category
		rows.Scan(&c.ID, &c.Name, &c.ProductCount)
		categories = append(categories, c)
	}

	renderTemplate(w, "categories.html", PageContext{
		CurrentUser: getCurrentUsername(r), Role: currentUser(r).Role,
		Data: categories,
	})
}

func CategoryAddHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		name := strings.TrimSpace(r.FormValue("name"))
		if name != "" {
			_, _ = DB.Exec("INSERT INTO categories (name) VALUES ($1) ON CONFLICT DO NOTHING", name)
		}
	}
	http.Redirect(w, r, "/categories", http.StatusSeeOther)
}

func CategoryDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	_, _ = DB.Exec("DELETE FROM categories WHERE id=$1", id)
	http.Redirect(w, r, "/categories", http.StatusSeeOther)
}

// 10. Бренди
func BrandsHandler(w http.ResponseWriter, r *http.Request) {
	rows, _ := DB.Query(`
		SELECT b.id, b.name, COUNT(p.id) 
		FROM brands b 
		LEFT JOIN products p ON b.id = p.brand_id 
		GROUP BY b.id, b.name 
		ORDER BY b.name
	`)
	defer rows.Close()

	var brands []Brand
	for rows.Next() {
		var b Brand
		rows.Scan(&b.ID, &b.Name, &b.ProductCount)
		brands = append(brands, b)
	}

	renderTemplate(w, "brands.html", PageContext{
		CurrentUser: getCurrentUsername(r), Role: currentUser(r).Role,
		Data: brands,
	})
}

func BrandAddHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		name := strings.TrimSpace(r.FormValue("name"))
		if name != "" {
			_, _ = DB.Exec("INSERT INTO brands (name) VALUES ($1) ON CONFLICT DO NOTHING", name)
		}
	}
	http.Redirect(w, r, "/brands", http.StatusSeeOther)
}

func BrandDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	_, _ = DB.Exec("DELETE FROM brands WHERE id=$1", id)
	http.Redirect(w, r, "/brands", http.StatusSeeOther)
}

func renderForm(w http.ResponseWriter, r *http.Request, p Product) {
	catRows, _ := DB.Query("SELECT id, name FROM categories ORDER BY name")
	defer catRows.Close()
	var categories []Category
	for catRows.Next() {
		var c Category
		catRows.Scan(&c.ID, &c.Name)
		categories = append(categories, c)
	}

	brandRows, _ := DB.Query("SELECT id, name FROM brands ORDER BY name")
	defer brandRows.Close()
	var brands []Brand
	for brandRows.Next() {
		var b Brand
		brandRows.Scan(&b.ID, &b.Name)
		brands = append(brands, b)
	}

	renderTemplate(w, "product_form.html", PageContext{
		CurrentUser: getCurrentUsername(r), Role: currentUser(r).Role,
		Data: FormPageData{
			Product:    p,
			Categories: categories,
			Brands:     brands,
		},
	})
}

func parseProductForm(w http.ResponseWriter, r *http.Request, existingImage string) (Product, error) {
	var p Product
	if err := r.ParseMultipartForm(6 << 20); err != nil {
		return p, fmt.Errorf("Форма завелика або некоректна")
	}
	p.Title = strings.TrimSpace(r.FormValue("title"))
	p.SKU = strings.TrimSpace(r.FormValue("sku"))
	var err error
	p.CategoryID, err = strconv.Atoi(r.FormValue("category_id"))
	if err != nil || p.CategoryID < 0 {
		return p, fmt.Errorf("Оберіть коректну категорію")
	}
	p.BrandID, err = strconv.Atoi(r.FormValue("brand_id"))
	if err != nil || p.BrandID < 0 {
		return p, fmt.Errorf("Оберіть коректний бренд")
	}
	p.PurchasePrice, err = strconv.ParseFloat(r.FormValue("purchase_price"), 64)
	if err != nil || math.IsNaN(p.PurchasePrice) || math.IsInf(p.PurchasePrice, 0) || p.PurchasePrice < 0 {
		return p, fmt.Errorf("Закупівельна ціна має бути невід’ємним числом")
	}
	p.SellingPrice, err = strconv.ParseFloat(r.FormValue("selling_price"), 64)
	if err != nil || math.IsNaN(p.SellingPrice) || math.IsInf(p.SellingPrice, 0) || p.SellingPrice < 0 {
		return p, fmt.Errorf("Ціна продажу має бути невід’ємним числом")
	}
	p.Quantity, err = strconv.Atoi(r.FormValue("quantity"))
	if err != nil || p.Quantity < 0 {
		return p, fmt.Errorf("Кількість має бути невід’ємним цілим числом")
	}
	p.WarrantyMonth, err = strconv.Atoi(r.FormValue("warranty_months"))
	if err != nil || p.WarrantyMonth < 0 {
		return p, fmt.Errorf("Гарантія має бути невід’ємним числом")
	}
	if p.Title == "" || p.SKU == "" {
		return p, fmt.Errorf("Назва та артикул обов’язкові")
	}
	p.ImagePath = existingImage
	file, _, err := r.FormFile("image")
	if err == http.ErrMissingFile {
		return p, nil
	}
	if err != nil {
		return p, fmt.Errorf("Не вдалося прочитати фото")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (5<<20)+1))
	if err != nil || len(data) > 5<<20 {
		return p, fmt.Errorf("Фото має бути не більшим за 5 МБ")
	}
	mime := http.DetectContentType(data)
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif"}[mime]
	if ext == "" {
		return p, fmt.Errorf("Дозволені формати фото: JPG, PNG, WEBP та GIF")
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return p, fmt.Errorf("Не вдалося зберегти фото")
	}
	name := hex.EncodeToString(token) + ext
	imagePath, err := saveProductImage(name, data, mime)
	if err != nil {
		return p, err
	}
	p.ImagePath = imagePath
	return p, nil
}

func UsersHandler(w http.ResponseWriter, r *http.Request) {
	if currentUser(r).Role != "admin" {
		http.Error(w, "Доступ лише для адміністратора", http.StatusForbidden)
		return
	}
	rows, err := DB.Query(`SELECT id, username, full_name, role FROM users ORDER BY id`)
	if err != nil {
		http.Error(w, "Не вдалося завантажити користувачів", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.FullName, &u.Role); err != nil {
			http.Error(w, "Помилка читання користувачів", http.StatusInternalServerError)
			return
		}
		users = append(users, u)
	}
	renderTemplate(w, "users.html", PageContext{CurrentUser: getCurrentUsername(r), Role: currentUser(r).Role, Data: UsersPageData{Users: users}})
}

func supabaseStorageSettings() (baseURL, serviceKey, bucket string) {
	baseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("SUPABASE_URL")), "/")
	serviceKey = strings.TrimSpace(os.Getenv("SUPABASE_SERVICE_ROLE_KEY"))
	bucket = envOr("SUPABASE_STORAGE_BUCKET", "product-images")
	return
}

func setSupabaseHeaders(req *http.Request, serviceKey string) {
	req.Header.Set("Authorization", "Bearer "+serviceKey)
	req.Header.Set("apikey", serviceKey)
}

func saveProductImage(name string, data []byte, mime string) (string, error) {
	baseURL, serviceKey, bucket := supabaseStorageSettings()
	if baseURL == "" && serviceKey == "" {
		if envOr("APP_ENV", "") == "production" {
			return "", fmt.Errorf("Налаштуйте SUPABASE_URL та SUPABASE_SERVICE_ROLE_KEY для постійного збереження фото")
		}
		if err := os.MkdirAll("uploads", 0755); err != nil {
			return "", fmt.Errorf("Не вдалося підготувати сховище фото")
		}
		if err := os.WriteFile(filepath.Join("uploads", name), data, 0644); err != nil {
			return "", fmt.Errorf("Не вдалося зберегти фото")
		}
		return "/uploads/" + name, nil
	}
	if baseURL == "" || serviceKey == "" {
		return "", fmt.Errorf("Неповні налаштування Supabase Storage")
	}
	endpoint := baseURL + "/storage/v1/object/" + url.PathEscape(bucket) + "/" + url.PathEscape(name)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("Не вдалося підготувати завантаження фото")
	}
	setSupabaseHeaders(req, serviceKey)
	req.Header.Set("Content-Type", mime)
	client := &http.Client{Timeout: 20 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Не вдалося завантажити фото до Supabase Storage: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("Supabase Storage відхилив фото (HTTP %s)", response.Status)
	}
	return baseURL + "/storage/v1/object/public/" + url.PathEscape(bucket) + "/" + url.PathEscape(name), nil
}

func removeProductImage(imagePath string) {
	if strings.HasPrefix(imagePath, "/uploads/") {
		name := strings.TrimPrefix(imagePath, "/uploads/")
		if name != "" && filepath.Base(name) == name {
			_ = os.Remove(filepath.Join("uploads", name))
		}
		return
	}
	base, key, bucket := supabaseStorageSettings()
	if base == "" || key == "" || bucket == "" {
		return
	}
	publicPrefix := base + "/storage/v1/object/public/" + bucket + "/"
	if !strings.HasPrefix(imagePath, publicPrefix) {
		return
	}
	objectName, err := url.PathUnescape(strings.TrimPrefix(imagePath, publicPrefix))
	if err != nil || objectName == "" || strings.Contains(objectName, "/") {
		return
	}
	body, _ := json.Marshal(map[string][]string{"prefixes": {objectName}})
	req, err := http.NewRequest(http.MethodDelete, base+"/storage/v1/object/"+url.PathEscape(bucket), bytes.NewReader(body))
	if err != nil {
		return
	}
	setSupabaseHeaders(req, key)
	req.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		log.Printf("Could not delete product image from Supabase Storage: %v", err)
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		log.Printf("Could not delete product image from Supabase Storage: HTTP %s", response.Status)
	}
}

func UserAddHandler(w http.ResponseWriter, r *http.Request) {
	if currentUser(r).Role != "admin" {
		http.Error(w, "Доступ лише для адміністратора", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	username, fullName, password := strings.TrimSpace(r.FormValue("username")), strings.TrimSpace(r.FormValue("full_name")), r.FormValue("password")
	if len(username) < 3 || len(username) > 50 || len(password) < 8 || len(fullName) == 0 {
		http.Error(w, "Вкажіть ім’я, логін від 3 символів та пароль від 8 символів", http.StatusBadRequest)
		return
	}
	if _, err := DB.Exec(`INSERT INTO users(username,password_hash,full_name,role) VALUES($1,$2,$3,'manager')`, username, hashPassword(password), fullName); err != nil {
		http.Error(w, "Не вдалося створити акаунт (логін має бути унікальним)", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}

func UserDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if currentUser(r).Role != "admin" {
		http.Error(w, "Доступ лише для адміністратора", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.Atoi(r.FormValue("id"))
	if err != nil {
		http.Error(w, "Некоректний ID", http.StatusBadRequest)
		return
	}
	user := currentUser(r)
	if user.ID == id {
		http.Error(w, "Не можна видалити власний акаунт", http.StatusBadRequest)
		return
	}
	if _, err = DB.Exec(`DELETE FROM users WHERE id=$1 AND role <> 'admin'`, id); err != nil {
		http.Error(w, "Не вдалося видалити користувача", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/users", http.StatusSeeOther)
}
