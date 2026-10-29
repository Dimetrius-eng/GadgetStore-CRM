package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"html/template"
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
	Quantity                    int
}

type DemoCustomer struct {
	ID          int64
	Name, Phone string
}

type DemoSale struct {
	ProductTitle, CustomerName, CreatedAt string
	Quantity                              int
	TotalPrice                            float64
}

type DemoChartPoint struct {
	Day     string
	Percent int
	Revenue float64
}

type DemoPageData struct {
	Products                                     []DemoProduct
	Customers                                    []DemoCustomer
	Sales                                        []DemoSale
	Revenue                                      float64
	ProductCount, CustomerCount, SaleCount       int
	TotalProducts                                int
	TotalInventoryVal, TotalRevenue, TotalProfit float64
	LowStockProducts                             []DemoProduct
	TopSales                                     []DemoTopSaleItem
	Chart                                        []DemoChartPoint
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
		{Title: "iPhone 15 Demo 128GB", SKU: "DEMO-IP15-128", Brand: "Apple", Category: "Смартфони", PurchasePrice: 28500, SellingPrice: 33999, Quantity: 8},
		{Title: "Samsung Galaxy S24 Demo 256GB", SKU: "DEMO-S24-256", Brand: "Samsung", Category: "Смартфони", PurchasePrice: 31000, SellingPrice: 36999, Quantity: 6},
		{Title: "AirPods Pro Demo", SKU: "DEMO-APP2", Brand: "Apple", Category: "Навушники", PurchasePrice: 8200, SellingPrice: 10999, Quantity: 3},
		{Title: "Galaxy Watch 6 Demo", SKU: "DEMO-GW6", Brand: "Samsung", Category: "Смарт-годинники", PurchasePrice: 7400, SellingPrice: 9999, Quantity: 2},
		{Title: "iPad Air Demo 256GB", SKU: "DEMO-IPADA-256", Brand: "Apple", Category: "Планшети", PurchasePrice: 27000, SellingPrice: 31999, Quantity: 5},
	}
	for _, p := range products {
		if _, err := tx.Exec(`INSERT INTO demo_products(session_token,title,sku,brand,category,purchase_price,selling_price,quantity)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, token, p.Title, p.SKU, p.Brand, p.Category, p.PurchasePrice, p.SellingPrice, p.Quantity); err != nil {
			return err
		}
	}
	customers := []DemoCustomer{{Name: "Олена Коваленко", Phone: "+380 00 000 00 01"}, {Name: "Андрій Мельник", Phone: "+380 00 000 00 02"}, {Name: "Марія Шевченко", Phone: "+380 00 000 00 03"}, {Name: "Дмитро Бондар", Phone: "+380 00 000 00 04"}}
	for _, c := range customers {
		if _, err := tx.Exec(`INSERT INTO demo_customers(session_token,name,phone) VALUES($1,$2,$3)`, token, c.Name, c.Phone); err != nil {
			return err
		}
	}
	// Sample sales use fictional names and dates so the dashboard and chart look populated.
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
	for _, table := range []string{"demo_sales", "demo_customers", "demo_products"} {
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
	rows, err := DB.Query(`SELECT id,title,sku,brand,category,purchase_price,selling_price,quantity FROM demo_products WHERE session_token=$1 ORDER BY id`, token)
	if err != nil {
		return data, err
	}
	for rows.Next() {
		var p DemoProduct
		if err := rows.Scan(&p.ID, &p.Title, &p.SKU, &p.Brand, &p.Category, &p.PurchasePrice, &p.SellingPrice, &p.Quantity); err != nil {
			rows.Close()
			return data, err
		}
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
	rows, err = DB.Query(`SELECT product_title,customer_name,quantity,total_price,created_at FROM demo_sales WHERE session_token=$1 ORDER BY created_at DESC,id DESC LIMIT 20`, token)
	if err != nil {
		return data, err
	}
	for rows.Next() {
		var s DemoSale
		var created time.Time
		if err := rows.Scan(&s.ProductTitle, &s.CustomerName, &s.Quantity, &s.TotalPrice, &created); err != nil {
			rows.Close()
			return data, err
		}
		s.CreatedAt = created.Format("02.01.2006")
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
	chartRows, err := DB.Query(`SELECT TO_CHAR(day,'DD Mon'), COALESCE(SUM(s.total_price),0)
		FROM generate_series(CURRENT_DATE-6,CURRENT_DATE,INTERVAL '1 day') AS day
		LEFT JOIN demo_sales s ON s.session_token=$1 AND s.created_at::date=day::date
		GROUP BY day ORDER BY day`, token)
	if err != nil {
		return data, err
	}
	maxRevenue := 0.0
	for chartRows.Next() {
		var point DemoChartPoint
		if err := chartRows.Scan(&point.Day, &point.Revenue); err != nil {
			chartRows.Close()
			return data, err
		}
		if point.Revenue > maxRevenue {
			maxRevenue = point.Revenue
		}
		data.Chart = append(data.Chart, point)
	}
	if err := chartRows.Err(); err != nil {
		chartRows.Close()
		return data, err
	}
	chartRows.Close()
	for i := range data.Chart {
		if maxRevenue == 0 {
			data.Chart[i].Percent = 8
		} else {
			data.Chart[i].Percent = int(math.Round(data.Chart[i].Revenue / maxRevenue * 100))
			if data.Chart[i].Percent < 8 {
				data.Chart[i].Percent = 8
			}
		}
	}
	data.ProductCount = len(data.Products)
	data.CustomerCount = len(data.Customers)
	data.TotalProducts = data.ProductCount
	for _, product := range data.Products {
		data.TotalInventoryVal += product.SellingPrice * float64(product.Quantity)
		if product.Quantity < 3 {
			data.LowStockProducts = append(data.LowStockProducts, product)
		}
	}
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
	p, err := parseDemoProduct(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err = DB.Exec(`INSERT INTO demo_products(session_token,title,sku,brand,category,purchase_price,selling_price,quantity) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, token, p.Title, p.SKU, p.Brand, p.Category, p.PurchasePrice, p.SellingPrice, p.Quantity); err != nil {
		http.Error(w, "Не вдалося додати товар. Артикул має бути унікальним.", http.StatusBadRequest)
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
	p, err := parseDemoProduct(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err = DB.Exec(`UPDATE demo_products SET title=$3,sku=$4,brand=$5,category=$6,purchase_price=$7,selling_price=$8,quantity=$9 WHERE session_token=$1 AND id=$2`, token, id, p.Title, p.SKU, p.Brand, p.Category, p.PurchasePrice, p.SellingPrice, p.Quantity); err != nil {
		http.Error(w, "Не вдалося оновити товар", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/demo#products", http.StatusSeeOther)
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
	http.Redirect(w, r, "/demo#customers", http.StatusSeeOther)
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
	http.Redirect(w, r, "/demo#customers", http.StatusSeeOther)
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
	http.Redirect(w, r, "/demo#customers", http.StatusSeeOther)
}

func DemoSaleCreateHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := demoPost(w, r)
	if !ok {
		return
	}
	productID, e1 := strconv.ParseInt(r.FormValue("product_id"), 10, 64)
	customerID, e2 := strconv.ParseInt(r.FormValue("customer_id"), 10, 64)
	quantity, e3 := strconv.Atoi(r.FormValue("quantity"))
	if e1 != nil || e2 != nil || e3 != nil || productID < 1 || customerID < 1 || quantity < 1 || quantity > 100 {
		http.Error(w, "Перевірте товар, клієнта та кількість", http.StatusBadRequest)
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
	if err = tx.QueryRow(`SELECT name FROM demo_customers WHERE session_token=$1 AND id=$2`, token, customerID).Scan(&customer); err != nil {
		http.Error(w, "Клієнта не знайдено в цьому демо", http.StatusBadRequest)
		return
	}
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
