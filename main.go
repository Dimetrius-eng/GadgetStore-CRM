package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	if envOr("APP_ENV", "") == "production" && len(strings.TrimSpace(os.Getenv("ORIGIN_VERIFY_SECRET"))) < 32 {
		log.Fatal("ORIGIN_VERIFY_SECRET must be configured in production with at least 32 characters")
	}
	// Ініціалізація підключення до бази даних PostgreSQL (127.0.0.1:5433)
	InitDB()
	startDemoCleanup()
	if err := os.MkdirAll("uploads", 0755); err != nil {
		log.Fatal(err)
	}
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

	// Публічний маршрут авторизації
	http.HandleFunc("/healthz", HealthHandler)
	http.HandleFunc("/login", LoginHandler)
	http.HandleFunc("/logout", LogoutHandler)
	http.HandleFunc("/overview", CRMOverviewHandler)
	http.HandleFunc("/demo", DemoHandler)
	http.HandleFunc("/demo/leave", DemoLeaveHandler)
	http.HandleFunc("/demo/keepalive", DemoKeepaliveHandler)
	http.HandleFunc("/demo/reset", DemoResetHandler)
	http.HandleFunc("/demo/products/add", DemoProductAddHandler)
	http.HandleFunc("/demo/products/edit", DemoProductEditHandler)
	http.HandleFunc("/demo/products/image", DemoProductImageHandler)
	http.HandleFunc("/demo/products/delete", DemoProductDeleteHandler)
	http.HandleFunc("/demo/categories/add", DemoCategoryAddHandler)
	http.HandleFunc("/demo/categories/delete", DemoCategoryDeleteHandler)
	http.HandleFunc("/demo/brands/add", DemoBrandAddHandler)
	http.HandleFunc("/demo/brands/delete", DemoBrandDeleteHandler)
	http.HandleFunc("/demo/users/add", DemoUserAddHandler)
	http.HandleFunc("/demo/users/delete", DemoUserDeleteHandler)
	http.HandleFunc("/demo/customers/add", DemoCustomerAddHandler)
	http.HandleFunc("/demo/customers/edit", DemoCustomerEditHandler)
	http.HandleFunc("/demo/customers/delete", DemoCustomerDeleteHandler)
	http.HandleFunc("/demo/sales/create", DemoSaleCreateHandler)
	http.HandleFunc("/demo/sales/receipt", DemoSaleReceiptHandler)
	http.HandleFunc("/session/activity", AuthMiddleware(SessionActivityHandler))

	// Захищені маршрути (вимагають входу в систему)
	http.HandleFunc("/", AuthMiddleware(IndexHandler))
	http.HandleFunc("/products", AuthMiddleware(ProductsHandler))
	http.HandleFunc("/products/add", AuthMiddleware(ProductAddHandler))
	http.HandleFunc("/products/edit", AuthMiddleware(ProductEditHandler))
	http.HandleFunc("/products/delete", AuthMiddleware(ProductDeleteHandler))

	// Каса та Чек
	http.HandleFunc("/sales", AuthMiddleware(SalesHandler))
	http.HandleFunc("/sales/create", AuthMiddleware(SaleCreateHandler))
	http.HandleFunc("/sales/receipt", AuthMiddleware(SaleReceiptHandler))

	// Категорії та Бренди
	http.HandleFunc("/categories", AuthMiddleware(CategoriesHandler))
	http.HandleFunc("/categories/add", AuthMiddleware(CategoryAddHandler))
	http.HandleFunc("/categories/delete", AuthMiddleware(CategoryDeleteHandler))

	http.HandleFunc("/brands", AuthMiddleware(BrandsHandler))
	http.HandleFunc("/brands/add", AuthMiddleware(BrandAddHandler))
	http.HandleFunc("/brands/delete", AuthMiddleware(BrandDeleteHandler))
	http.HandleFunc("/users", AuthMiddleware(UsersHandler))
	http.HandleFunc("/users/add", AuthMiddleware(UserAddHandler))
	http.HandleFunc("/users/delete", AuthMiddleware(UserDeleteHandler))

	port := envOr("PORT", envOr("APP_PORT", "8080"))
	fmt.Printf("🚀 Веб-сайт запущено! Відкрийте у браузері: http://localhost:%s\n", port)
	handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(r.Host)
		if host == "gadget-store-crm.tech" || host == "www.gadget-store-crm.tech" {
			http.Redirect(w, r, "https://dimetrius-eng.tech/gadget-store-crm/overview", http.StatusMovedPermanently)
			return
		}
		http.DefaultServeMux.ServeHTTP(w, r)
	}))
	handler = sameOriginMutationGuard(handler)
	handler = securityHeaders(handler)
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	log.Fatal(server.ListenAndServe())
}

// HealthHandler is intentionally unauthenticated for the hosting platform's
// direct health probe. It returns no service or database details.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
