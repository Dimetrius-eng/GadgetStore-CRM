package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	// Ініціалізація підключення до бази даних PostgreSQL (127.0.0.1:5433)
	InitDB()
	if err := os.MkdirAll("uploads", 0755); err != nil {
		log.Fatal(err)
	}
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

	// Публічний маршрут авторизації
	http.HandleFunc("/login", LoginHandler)
	http.HandleFunc("/logout", LogoutHandler)

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
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
