package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"html/template"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const demoIdleTimeout = 45 * time.Minute

func startDemoCleanup() {
	pruneExpiredDemoSessions()
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			pruneExpiredDemoSessions()
		}
	}()
}

func pruneExpiredDemoSessions() {
	if _, err := DB.Exec(`DELETE FROM demo_sessions WHERE expires_at <= NOW()`); err != nil {
		log.Printf("Could not clean expired demo sessions: %v", err)
	}
}

type DemoProduct struct {
	ID                          int64
	Title, SKU, Brand, Category string
	PurchasePrice, SellingPrice float64
	Quantity, WarrantyMonth     int
	ImageContentType            string
	HasImage                    bool
}

type DemoCustomer struct {
	ID          int64
	Name, Phone string
}

type DemoSale struct {
	ID                                    int64
	ProductTitle, CustomerName, CreatedAt string
	Quantity                              int
	TotalPrice, Profit                    float64
}

type DemoNamedEntity struct {
	ID           int64
	Name         string
	ProductCount int
}

type DemoPageData struct {
	Products                                     []DemoProduct
	VisibleProducts                              []DemoProduct
	Customers                                    []DemoCustomer
	Sales                                        []DemoSale
	Categories, Brands                           []DemoNamedEntity
	SearchQuery                                  string
	Revenue                                      float64
	ProductCount, CustomerCount, SaleCount       int
	TotalProducts                                int
	TotalInventoryVal, TotalRevenue, TotalProfit float64
	LowStockProducts                             []DemoProduct
	TopSales                                     []DemoTopSaleItem
	DemoUsers                                    []DemoUser
}

type DemoUser struct {
	ID                   int64
	Name, Username, Role string
}

type DemoTopSaleItem struct {
	Title        string
	TotalSold    int
	TotalRevenue float64
}

func CRMOverviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := template.Must(template.ParseFiles("templates/overview.html")).Execute(w, nil); err != nil {
		http.Error(w, "Не вдалося відобразити сторінку", http.StatusInternalServerError)
	}
}

func DemoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token, ok := ensureDemoSession(w, r)
	if !ok {
		http.Error(w, "Не вдалося створити демо-сесію", http.StatusInternalServerError)
		return
	}
	data, err := loadDemoPage(token)
	if err != nil {
		http.Error(w, "Не вдалося завантажити демо-дані", http.StatusInternalServerError)
		return
	}
	data.SearchQuery = strings.TrimSpace(r.URL.Query().Get("search"))
	if data.SearchQuery != "" {
		filtered := make([]DemoProduct, 0, len(data.Products))
		query := strings.ToLower(data.SearchQuery)
		for _, product := range data.Products {
			if strings.Contains(strings.ToLower(product.Title), query) || strings.Contains(strings.ToLower(product.SKU), query) {
				filtered = append(filtered, product)
			}
		}
		data.VisibleProducts = filtered
	}
	w.Header().Set("Cache-Control", "no-store")
	renderTemplate(w, "demo.html", PageContext{CurrentUser: "Демо-перегляд", Role: "demo", DemoMode: true, Data: data})
}

func ensureDemoSession(w http.ResponseWriter, r *http.Request) (string, bool) {
	_, _ = DB.Exec(`DELETE FROM demo_sessions WHERE expires_at <= NOW()`)
	if cookie, err := r.Cookie("demo_session"); err == nil && len(cookie.Value) == 64 {
		var token string
		err := DB.QueryRow(`UPDATE demo_sessions SET last_seen_at=NOW(), expires_at=NOW()+($2 * INTERVAL '1 second')
			WHERE token=$1 AND expires_at>NOW() RETURNING token`, cookie.Value, int64(demoIdleTimeout.Seconds())).Scan(&token)
		if err == nil {
			setDemoCookie(w, token)
			return token, true
		}
	}

	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", false
	}
	token := hex.EncodeToString(bytes)
	tx, err := DB.Begin()
	if err != nil {
		return "", false
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO demo_sessions(token, expires_at) VALUES($1, NOW()+($2 * INTERVAL '1 second'))`, token, int64(demoIdleTimeout.Seconds())); err != nil {
		return "", false
	}
	if err = seedDemoData(tx, token); err != nil {
		return "", false
	}
	if err = tx.Commit(); err != nil {
		return "", false
	}
	setDemoCookie(w, token)
	return token, true
}

func setDemoCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: "demo_session", Value: token, Path: "/", HttpOnly: true,
		Secure: envOr("APP_ENV", "") == "production", SameSite: http.SameSiteLaxMode,
		MaxAge: int(demoIdleTimeout.Seconds()),
	})
}

func seedDemoData(tx *sql.Tx, token string) error {
	products := []DemoProduct{
		{Title: "iPhone 15 Demo 128GB", SKU: "DEMO-IP15-128", Brand: "Apple", Category: "Смартфони", PurchasePrice: 28500, SellingPrice: 33999, Quantity: 8, WarrantyMonth: 12},
		{Title: "Samsung Galaxy S24 Demo 256GB", SKU: "DEMO-S24-256", Brand: "Samsung", Category: "Смартфони", PurchasePrice: 31000, SellingPrice: 36999, Quantity: 6, WarrantyMonth: 24},
		{Title: "AirPods Pro Demo", SKU: "DEMO-APP2", Brand: "Apple", Category: "Навушники", PurchasePrice: 8200, SellingPrice: 10999, Quantity: 3, WarrantyMonth: 12},
		{Title: "Galaxy Watch 6 Demo", SKU: "DEMO-GW6", Brand: "Samsung", Category: "Смарт-годинники", PurchasePrice: 7400, SellingPrice: 9999, Quantity: 2, WarrantyMonth: 12},
		{Title: "iPad Air Demo 256GB", SKU: "DEMO-IPADA-256", Brand: "Apple", Category: "Планшети", PurchasePrice: 27000, SellingPrice: 31999, Quantity: 5, WarrantyMonth: 12},
	}
	for _, p := range products {
		if _, err := tx.Exec(`INSERT INTO demo_products(session_token,title,sku,brand,category,purchase_price,selling_price,quantity,warranty_months)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, token, p.Title, p.SKU, p.Brand, p.Category, p.PurchasePrice, p.SellingPrice, p.Quantity, p.WarrantyMonth); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO demo_categories(session_token,name) VALUES($1,$2) ON CONFLICT DO NOTHING`, token, p.Category); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO demo_brands(session_token,name) VALUES($1,$2) ON CONFLICT DO NOTHING`, token, p.Brand); err != nil {
			return err
		}
	}
	users := []DemoUser{{Name: "Демо-адміністратор", Username: "demo.admin", Role: "admin"}, {Name: "Демо-менеджер", Username: "demo.manager", Role: "manager"}}
	for _, user := range users {
		if _, err := tx.Exec(`INSERT INTO demo_users(session_token,full_name,username,role) VALUES($1,$2,$3,$4)`, token, user.Name, user.Username, user.Role); err != nil {
			return err
		}
	}
	customers := []DemoCustomer{{Name: "Олена Коваленко", Phone: "+380 00 000 00 01"}, {Name: "Андрій Мельник", Phone: "+380 00 000 00 02"}, {Name: "Марія Шевченко", Phone: "+380 00 000 00 03"}, {Name: "Дмитро Бондар", Phone: "+380 00 000 00 04"}}
	for _, c := range customers {
		if _, err := tx.Exec(`INSERT INTO demo_customers(session_token,name,phone) VALUES($1,$2,$3)`, token, c.Name, c.Phone); err != nil {
			return err
		}
	}
	// Sample sales use fictional names and dates so the dashboard looks populated.
	sales := []struct {
		product, customer string
		quantity          int
		total             float64
		days              int
	}{
		{"iPhone 15 Demo 128GB", "Олена Коваленко", 1, 33999, 0},
		{"Samsung Galaxy S24 Demo 256GB", "Андрій Мельник", 1, 36999, 1},
		{"AirPods Pro Demo", "Марія Шевченко", 2, 21998, 2},
		{"iPad Air Demo 256GB", "Дмитро Бондар", 1, 31999, 3},
		{"iPhone 15 Demo 128GB", "Марія Шевченко", 1, 33999, 4},
		{"Galaxy Watch 6 Demo", "Олена Коваленко", 1, 9999, 5},
		{"Samsung Galaxy S24 Demo 256GB", "Дмитро Бондар", 1, 36999, 6},
	}
	for _, sale := range sales {
		if _, err := tx.Exec(`INSERT INTO demo_sales(session_token,product_title,customer_name,quantity,unit_price,total_price,created_at)
			VALUES($1,$2,$3,$4,$5,$6,NOW()-($7 * INTERVAL '1 day'))`, token, sale.product, sale.customer, sale.quantity, sale.total/float64(sale.quantity), sale.total, sale.days); err != nil {
			return err
		}
	}
	return nil
}

func resetDemoData(token string) error {
	tx, err := DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"demo_sales", "demo_customers", "demo_products", "demo_categories", "demo_brands", "demo_users"} {
		if _, err = tx.Exec(`DELETE FROM `+table+` WHERE session_token=$1`, token); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`UPDATE demo_sessions SET last_seen_at=NOW(),expires_at=NOW()+($2 * INTERVAL '1 second') WHERE token=$1`, token, int64(demoIdleTimeout.Seconds())); err != nil {
		return err
	}
	if err = seedDemoData(tx, token); err != nil {
		return err
	}
	return tx.Commit()
}

func loadDemoPage(token string) (DemoPageData, error) {
	var data DemoPageData
	if err := ensureDemoCatalogLists(token); err != nil {
		return data, err
	}
	rows, err := DB.Query(`SELECT id,title,sku,brand,category,purchase_price,selling_price,quantity,warranty_months,COALESCE(image_content_type,'') FROM demo_products WHERE session_token=$1 ORDER BY id`, token)
	if err != nil {
		return data, err
	}
	for rows.Next() {
		var p DemoProduct
		if err := rows.Scan(&p.ID, &p.Title, &p.SKU, &p.Brand, &p.Category, &p.PurchasePrice, &p.SellingPrice, &p.Quantity, &p.WarrantyMonth, &p.ImageContentType); err != nil {
			rows.Close()
			return data, err
		}
		p.HasImage = p.ImageContentType != ""
		data.Products = append(data.Products, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return data, err
	}
	rows.Close()
	rows, err = DB.Query(`SELECT id,name,phone FROM demo_customers WHERE session_token=$1 ORDER BY id`, token)
	if err != nil {
		return data, err
	}
	for rows.Next() {
		var c DemoCustomer
		if err := rows.Scan(&c.ID, &c.Name, &c.Phone); err != nil {
			rows.Close()
			return data, err
		}
		data.Customers = append(data.Customers, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return data, err
	}
	rows.Close()
	rows, err = DB.Query(`SELECT s.id,s.product_title,s.customer_name,s.quantity,s.total_price,
		COALESCE(s.total_price-COALESCE(p.purchase_price,0)*s.quantity,0),s.created_at
		FROM demo_sales s LEFT JOIN demo_products p ON p.session_token=s.session_token AND p.title=s.product_title
		WHERE s.session_token=$1 ORDER BY s.created_at DESC,s.id DESC LIMIT 20`, token)
	if err != nil {
		return data, err
	}
	for rows.Next() {
		var s DemoSale
		var created time.Time
		if err := rows.Scan(&s.ID, &s.ProductTitle, &s.CustomerName, &s.Quantity, &s.TotalPrice, &s.Profit, &created); err != nil {
			rows.Close()
			return data, err
		}
		s.CreatedAt = created.Format("02.01.2006 15:04")
		data.Sales = append(data.Sales, s)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return data, err
	}
	rows.Close()
	if err := DB.QueryRow(`SELECT COUNT(*),COALESCE(SUM(total_price),0) FROM demo_sales WHERE session_token=$1`, token).Scan(&data.SaleCount, &data.Revenue); err != nil {
		return data, err
	}
	data.ProductCount = len(data.Products)
	data.CustomerCount = len(data.Customers)
	data.TotalProducts = data.ProductCount
	for _, product := range data.Products {
		data.VisibleProducts = append(data.VisibleProducts, product)
		data.TotalInventoryVal += product.SellingPrice * float64(product.Quantity)
		if product.Quantity < 3 {
			data.LowStockProducts = append(data.LowStockProducts, product)
		}
	}
	categoryRows, err := DB.Query(`SELECT c.id,c.name,COUNT(p.id) FROM demo_categories c LEFT JOIN demo_products p ON p.session_token=c.session_token AND p.category=c.name WHERE c.session_token=$1 GROUP BY c.id,c.name ORDER BY c.name`, token)
	if err != nil {
		return data, err
	}
	for categoryRows.Next() {
		var item DemoNamedEntity
		if err := categoryRows.Scan(&item.ID, &item.Name, &item.ProductCount); err != nil {
			categoryRows.Close()
			return data, err
		}
		data.Categories = append(data.Categories, item)
	}
	if err := categoryRows.Err(); err != nil {
		categoryRows.Close()
		return data, err
	}
	categoryRows.Close()
	brandRows, err := DB.Query(`SELECT b.id,b.name,COUNT(p.id) FROM demo_brands b LEFT JOIN demo_products p ON p.session_token=b.session_token AND p.brand=b.name WHERE b.session_token=$1 GROUP BY b.id,b.name ORDER BY b.name`, token)
	if err != nil {
		return data, err
	}
	for brandRows.Next() {
		var item DemoNamedEntity
		if err := brandRows.Scan(&item.ID, &item.Name, &item.ProductCount); err != nil {
			brandRows.Close()
			return data, err
		}
		data.Brands = append(data.Brands, item)
	}
	if err := brandRows.Err(); err != nil {
		brandRows.Close()
		return data, err
	}
	brandRows.Close()
	userRows, err := DB.Query(`SELECT id,full_name,username,role FROM demo_users WHERE session_token=$1 ORDER BY id`, token)
	if err != nil {
		return data, err
	}
	for userRows.Next() {
		var user DemoUser
		if err := userRows.Scan(&user.ID, &user.Name, &user.Username, &user.Role); err != nil {
			userRows.Close()
			return data, err
		}
		data.DemoUsers = append(data.DemoUsers, user)
	}
	if err := userRows.Err(); err != nil {
		userRows.Close()
		return data, err
	}
	userRows.Close()
	data.TotalRevenue = data.Revenue
	if err := DB.QueryRow(`SELECT COALESCE(SUM(s.total_price-COALESCE(p.purchase_price,0)*s.quantity),0)
		FROM demo_sales s LEFT JOIN demo_products p ON p.session_token=s.session_token AND p.title=s.product_title
		WHERE s.session_token=$1`, token).Scan(&data.TotalProfit); err != nil {
		return data, err
	}
	topRows, err := DB.Query(`SELECT product_title,SUM(quantity),SUM(total_price) FROM demo_sales WHERE session_token=$1 GROUP BY product_title ORDER BY SUM(quantity) DESC LIMIT 5`, token)
	if err != nil {
		return data, err
	}
	for topRows.Next() {
		var item DemoTopSaleItem
		if err := topRows.Scan(&item.Title, &item.TotalSold, &item.TotalRevenue); err != nil {
			topRows.Close()
			return data, err
		}
		data.TopSales = append(data.TopSales, item)
	}
	if err := topRows.Err(); err != nil {
		topRows.Close()
		return data, err
	}
	topRows.Close()
	return data, nil
}

func ensureDemoCatalogLists(token string) error {
	if _, err := DB.Exec(`INSERT INTO demo_categories(session_token,name) SELECT DISTINCT session_token,category FROM demo_products WHERE session_token=$1 ON CONFLICT DO NOTHING`, token); err != nil {
		return err
	}
	if _, err := DB.Exec(`INSERT INTO demo_brands(session_token,name) SELECT DISTINCT session_token,brand FROM demo_products WHERE session_token=$1 ON CONFLICT DO NOTHING`, token); err != nil {
		return err
	}
	_, err := DB.Exec(`INSERT INTO demo_users(session_token,full_name,username,role)
		SELECT $1,'Демо-адміністратор','demo.admin','admin'
		WHERE NOT EXISTS (SELECT 1 FROM demo_users WHERE session_token=$1)`, token)
	return err
}

func demoPost(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return "", false
	}
	token, ok := ensureDemoSession(w, r)
	if !ok {
		http.Error(w, "Не вдалося відкрити демо-сесію", http.StatusInternalServerError)
	}
	return token, ok
}

func DemoResetHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := demoPost(w, r)
	if !ok {
		return
	}
	if err := resetDemoData(token); err != nil {
		http.Error(w, "Не вдалося очистити тестові дані", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/demo?reset=1", http.StatusSeeOther)
}

func DemoLeaveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if cookie, err := r.Cookie("demo_session"); err == nil && len(cookie.Value) == 64 {
		// Give a reload or quick return a brief grace period; a normal page load
		// refreshes the 45-minute idle lease. Closed tabs are then purged lazily.
		_, _ = DB.Exec(`UPDATE demo_sessions SET expires_at=NOW()+INTERVAL '2 minutes' WHERE token=$1`, cookie.Value)
	}
	w.WriteHeader(http.StatusNoContent)
}

func DemoKeepaliveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := ensureDemoSession(w, r); !ok {
		http.Error(w, "Не вдалося оновити демо-сесію", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func DemoProductAddHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := demoPost(w, r)
	if !ok {
		return
	}
	imageData, imageType, err := readDemoProductImage(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p, err := parseDemoProduct(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err = DB.Exec(`INSERT INTO demo_products(session_token,title,sku,brand,category,purchase_price,selling_price,quantity,warranty_months,image_data,image_content_type) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, token, p.Title, p.SKU, p.Brand, p.Category, p.PurchasePrice, p.SellingPrice, p.Quantity, p.WarrantyMonth, nullableDemoImage(imageData), nullableDemoImageType(imageType)); err != nil {
		http.Error(w, "Не вдалося додати товар. Артикул має бути унікальним.", http.StatusBadRequest)
		return
	}
	if err = ensureDemoProductLabels(token, p); err != nil {
		http.Error(w, "Не вдалося оновити демонстраційні категорії та бренди", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/demo#products", http.StatusSeeOther)
}

func DemoProductEditHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := demoPost(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id < 1 {
		http.Error(w, "Некоректний товар", http.StatusBadRequest)
		return
	}
	imageData, imageType, err := readDemoProductImage(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p, err := parseDemoProduct(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	removeImage := r.FormValue("remove_image") == "1"
	imageChanged := len(imageData) > 0 || removeImage
	if _, err = DB.Exec(`UPDATE demo_products SET title=$3,sku=$4,brand=$5,category=$6,purchase_price=$7,selling_price=$8,quantity=$9,warranty_months=$10,
		image_data=CASE WHEN $11 THEN $12 ELSE image_data END,
		image_content_type=CASE WHEN $11 THEN $13 ELSE image_content_type END
		WHERE session_token=$1 AND id=$2`, token, id, p.Title, p.SKU, p.Brand, p.Category, p.PurchasePrice, p.SellingPrice, p.Quantity, p.WarrantyMonth, imageChanged, nullableDemoImage(imageData), nullableDemoImageType(imageType)); err != nil {
		http.Error(w, "Не вдалося оновити товар", http.StatusBadRequest)
		return
	}
	if err = ensureDemoProductLabels(token, p); err != nil {
		http.Error(w, "Не вдалося оновити демонстраційні категорії та бренди", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/demo#products", http.StatusSeeOther)
}

const demoProductImageLimit = 5 << 20

func readDemoProductImage(w http.ResponseWriter, r *http.Request) ([]byte, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, demoProductImageLimit+(1<<20))
	file, _, err := r.FormFile("image")
	if err != nil {
		if err == http.ErrMissingFile || err == http.ErrNotMultipart {
			return nil, "", nil
		}
		if _, ok := err.(*http.MaxBytesError); ok {
			return nil, "", errDemo("Розмір запиту завеликий")
		}
		return nil, "", errDemo("Не вдалося прочитати фото")
	}
	defer file.Close()
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	data, err := io.ReadAll(io.LimitReader(file, demoProductImageLimit+1))
	if err != nil {
		return nil, "", errDemo("Не вдалося прочитати фото")
	}
	if len(data) == 0 {
		return nil, "", errDemo("Вибраний файл фото порожній")
	}
	if len(data) > demoProductImageLimit {
		return nil, "", errDemo("Фото має бути не більшим за 5 МБ")
	}
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
		return data, contentType, nil
	default:
		return nil, "", errDemo("Підтримуються фото JPG, PNG, WEBP або GIF")
	}
}

func nullableDemoImage(data []byte) any {
	if len(data) == 0 {
		return nil
	}
	return data
}

func nullableDemoImageType(contentType string) any {
	if contentType == "" {
		return nil
	}
	return contentType
}

func DemoProductImageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cookie, err := r.Cookie("demo_session")
	if err != nil || len(cookie.Value) != 64 {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	var imageData []byte
	var contentType string
	err = DB.QueryRow(`SELECT p.image_data,p.image_content_type FROM demo_products p
		JOIN demo_sessions s ON s.token=p.session_token AND s.expires_at>NOW()
		WHERE p.session_token=$1 AND p.id=$2 AND p.image_data IS NOT NULL`, cookie.Value, id).Scan(&imageData, &contentType)
	if err != nil || len(imageData) == 0 {
		http.NotFound(w, r)
		return
	}
	switch contentType {
	case "image/jpeg", "image/png", "image/webp", "image/gif":
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(imageData)))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodHead {
		_, _ = w.Write(imageData)
	}
}

func DemoProductDeleteHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := demoPost(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id < 1 {
		http.Error(w, "Некоректний товар", http.StatusBadRequest)
		return
	}
	_, err = DB.Exec(`DELETE FROM demo_products WHERE session_token=$1 AND id=$2`, token, id)
	if err != nil {
		http.Error(w, "Не вдалося видалити товар", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/demo#products", http.StatusSeeOther)
}

func ensureDemoProductLabels(token string, product DemoProduct) error {
	if _, err := DB.Exec(`INSERT INTO demo_categories(session_token,name) VALUES($1,$2) ON CONFLICT DO NOTHING`, token, product.Category); err != nil {
		return err
	}
	_, err := DB.Exec(`INSERT INTO demo_brands(session_token,name) VALUES($1,$2) ON CONFLICT DO NOTHING`, token, product.Brand)
	return err
}

func demoNameAdd(w http.ResponseWriter, r *http.Request, table, tab string) {
	token, ok := demoPost(w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || len(name) > 100 {
		http.Error(w, "Перевірте назву", http.StatusBadRequest)
		return
	}
	if _, err := DB.Exec(`INSERT INTO `+table+`(session_token,name) VALUES($1,$2) ON CONFLICT DO NOTHING`, token, name); err != nil {
		http.Error(w, "Не вдалося зберегти назву", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/demo#"+tab, http.StatusSeeOther)
}

func demoNameDelete(w http.ResponseWriter, r *http.Request, table, tab string) {
	token, ok := demoPost(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id < 1 {
		http.Error(w, "Некоректний запис", http.StatusBadRequest)
		return
	}
	if _, err = DB.Exec(`DELETE FROM `+table+` WHERE session_token=$1 AND id=$2`, token, id); err != nil {
		http.Error(w, "Не вдалося видалити запис", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/demo#"+tab, http.StatusSeeOther)
}

func DemoCategoryAddHandler(w http.ResponseWriter, r *http.Request) {
	demoNameAdd(w, r, "demo_categories", "categories")
}
func DemoCategoryDeleteHandler(w http.ResponseWriter, r *http.Request) {
	demoNameDelete(w, r, "demo_categories", "categories")
}
func DemoBrandAddHandler(w http.ResponseWriter, r *http.Request) {
	demoNameAdd(w, r, "demo_brands", "brands")
}
func DemoBrandDeleteHandler(w http.ResponseWriter, r *http.Request) {
	demoNameDelete(w, r, "demo_brands", "brands")
}

func DemoUserAddHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := demoPost(w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.FormValue("full_name"))
	username := strings.TrimSpace(r.FormValue("username"))
	if name == "" || len(name) > 100 || len(username) < 3 || len(username) > 50 {
		http.Error(w, "Перевірте ім’я та демонстраційний логін", http.StatusBadRequest)
		return
	}
	if _, err := DB.Exec(`INSERT INTO demo_users(session_token,full_name,username,role) VALUES($1,$2,$3,'manager')`, token, name, username); err != nil {
		http.Error(w, "Не вдалося створити демо-користувача. Логін має бути унікальним.", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/demo#users", http.StatusSeeOther)
}

func DemoUserDeleteHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := demoPost(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id < 1 {
		http.Error(w, "Некоректний користувач", http.StatusBadRequest)
		return
	}
	if _, err = DB.Exec(`DELETE FROM demo_users WHERE session_token=$1 AND id=$2 AND role<>'admin'`, token, id); err != nil {
		http.Error(w, "Не вдалося видалити демо-користувача", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/demo#users", http.StatusSeeOther)
}

func parseDemoProduct(r *http.Request) (DemoProduct, error) {
	var p DemoProduct
	p.Title = strings.TrimSpace(r.FormValue("title"))
	p.SKU = strings.TrimSpace(r.FormValue("sku"))
	p.Brand = strings.TrimSpace(r.FormValue("brand"))
	p.Category = strings.TrimSpace(r.FormValue("category"))
	var err error
	p.PurchasePrice, err = strconv.ParseFloat(r.FormValue("purchase_price"), 64)
	if err != nil || math.IsNaN(p.PurchasePrice) || math.IsInf(p.PurchasePrice, 0) || p.PurchasePrice < 0 {
		return p, errDemo("Некоректна закупівельна ціна")
	}
	p.SellingPrice, err = strconv.ParseFloat(r.FormValue("selling_price"), 64)
	if err != nil || math.IsNaN(p.SellingPrice) || math.IsInf(p.SellingPrice, 0) || p.SellingPrice < 0 {
		return p, errDemo("Некоректна ціна продажу")
	}
	p.Quantity, err = strconv.Atoi(r.FormValue("quantity"))
	if err != nil || p.Quantity < 0 {
		return p, errDemo("Кількість має бути невід’ємним цілим числом")
	}
	p.WarrantyMonth, err = strconv.Atoi(r.FormValue("warranty_months"))
	if err != nil || p.WarrantyMonth < 0 {
		return p, errDemo("Термін гарантії має бути невід’ємним числом")
	}
	if p.Title == "" || p.SKU == "" || p.Brand == "" || p.Category == "" {
		return p, errDemo("Заповніть назву, артикул, бренд і категорію")
	}
	if len(p.Title) > 255 || len(p.SKU) > 100 || len(p.Brand) > 100 || len(p.Category) > 100 {
		return p, errDemo("Завелика довжина одного з полів")
	}
	return p, nil
}

type demoError string

func (e demoError) Error() string { return string(e) }
func errDemo(s string) error      { return demoError(s) }

func DemoCustomerAddHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := demoPost(w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	phone := strings.TrimSpace(r.FormValue("phone"))
	if name == "" || len(name) > 160 || len(phone) > 32 {
		http.Error(w, "Перевірте ім’я та телефон клієнта", http.StatusBadRequest)
		return
	}
	if _, err := DB.Exec(`INSERT INTO demo_customers(session_token,name,phone) VALUES($1,$2,$3)`, token, name, phone); err != nil {
		http.Error(w, "Не вдалося додати клієнта", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/demo#sales", http.StatusSeeOther)
}

func DemoCustomerEditHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := demoPost(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	name := strings.TrimSpace(r.FormValue("name"))
	phone := strings.TrimSpace(r.FormValue("phone"))
	if err != nil || id < 1 || name == "" || len(name) > 160 || len(phone) > 32 {
		http.Error(w, "Перевірте дані клієнта", http.StatusBadRequest)
		return
	}
	if _, err = DB.Exec(`UPDATE demo_customers SET name=$3,phone=$4 WHERE session_token=$1 AND id=$2`, token, id, name, phone); err != nil {
		http.Error(w, "Не вдалося оновити клієнта", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/demo#sales", http.StatusSeeOther)
}

func DemoCustomerDeleteHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := demoPost(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id < 1 {
		http.Error(w, "Некоректний клієнт", http.StatusBadRequest)
		return
	}
	_, err = DB.Exec(`DELETE FROM demo_customers WHERE session_token=$1 AND id=$2`, token, id)
	if err != nil {
		http.Error(w, "Не вдалося видалити клієнта", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/demo#sales", http.StatusSeeOther)
}

func DemoSaleCreateHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := demoPost(w, r)
	if !ok {
		return
	}
	productID, e1 := strconv.ParseInt(r.FormValue("product_id"), 10, 64)
	quantity, e3 := strconv.Atoi(r.FormValue("quantity"))
	customerPhone, phoneErr := normalizeCustomerPhone(r.FormValue("customer_phone"))
	if e1 != nil || e3 != nil || productID < 1 || quantity < 1 || quantity > 100 {
		http.Error(w, "Перевірте товар і кількість", http.StatusBadRequest)
		return
	}
	if phoneErr != nil {
		http.Error(w, phoneErr.Error(), http.StatusBadRequest)
		return
	}
	tx, err := DB.Begin()
	if err != nil {
		http.Error(w, "Помилка бази даних", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	var title, customer string
	var price float64
	err = tx.QueryRow(`SELECT title,selling_price FROM demo_products WHERE session_token=$1 AND id=$2 FOR UPDATE`, token, productID).Scan(&title, &price)
	if err != nil {
		http.Error(w, "Товар не знайдено в цьому демо", http.StatusBadRequest)
		return
	}
	customer = customerPhone
	result, err := tx.Exec(`UPDATE demo_products SET quantity=quantity-$3 WHERE session_token=$1 AND id=$2 AND quantity >= $3`, token, productID, quantity)
	if err != nil {
		http.Error(w, "Не вдалося оновити залишок", http.StatusInternalServerError)
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		http.Error(w, "Недостатньо товару на демо-складі", http.StatusBadRequest)
		return
	}
	total := price * float64(quantity)
	if _, err = tx.Exec(`INSERT INTO demo_sales(session_token,product_title,customer_name,quantity,unit_price,total_price) VALUES($1,$2,$3,$4,$5,$6)`, token, title, customer, quantity, price, total); err != nil {
		http.Error(w, "Не вдалося записати демо-продаж", http.StatusInternalServerError)
		return
	}
	if err = tx.Commit(); err != nil {
		http.Error(w, "Не вдалося зберегти демо-продаж", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/demo#sales", http.StatusSeeOther)
}

func DemoSaleReceiptHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token, ok := ensureDemoSession(w, r)
	if !ok {
		http.Error(w, "Не вдалося відкрити демо-сесію", http.StatusInternalServerError)
		return
	}
	id, err := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	var sale Sale
	err = DB.QueryRow(`SELECT s.id,s.product_title,s.quantity,s.unit_price,s.total_price,
		COALESCE(s.total_price-COALESCE(p.purchase_price,0)*s.quantity,0),TO_CHAR(s.created_at,'DD.MM.YYYY HH24:MI')
		FROM demo_sales s LEFT JOIN demo_products p ON p.session_token=s.session_token AND p.title=s.product_title
		WHERE s.session_token=$1 AND s.id=$2`, token, id).Scan(&sale.ID, &sale.ProductTitle, &sale.QuantitySold, &sale.UnitPrice, &sale.TotalPrice, &sale.Profit, &sale.CreatedAtFormatted)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	tmpl, err := template.New("receipt.html").Funcs(funcMap).ParseFiles("templates/receipt.html")
	if err != nil {
		http.Error(w, "Не вдалося сформувати демо-чек", http.StatusInternalServerError)
		return
	}
	if err := tmpl.Execute(w, sale); err != nil {
		log.Printf("Could not render demo receipt: %v", err)
	}
}
