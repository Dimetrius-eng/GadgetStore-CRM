package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatMoney(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want string
	}{
		{"rounds binary float to cents", 1.15, "1.15 ₴"},
		{"rounds small binary float", 0.29, "0.29 ₴"},
		{"rounds half cent", 1.005, "1.01 ₴"},
		{"negative fraction keeps sign", -0.5, "-0.50 ₴"},
		{"negative cent keeps sign", -0.01, "-0.01 ₴"},
		{"groups thousands", 1234567.89, "1 234 567.89 ₴"},
		{"zero", 0, "0.00 ₴"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatMoney(tt.in); got != tt.want {
				t.Errorf("formatMoney(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatMoneyRejectsNonFiniteValues(t *testing.T) {
	for _, value := range []float64{1e300, -1e300} {
		if got := formatMoney(value); got != "—" {
			t.Errorf("formatMoney(%v) = %q, want em dash", value, got)
		}
	}
}

func TestValidSaleQuantity(t *testing.T) {
	for _, tt := range []struct {
		name      string
		sold, in  int
		wantValid bool
	}{
		{"one item in stock", 1, 1, true},
		{"less than available", 2, 5, true},
		{"exceeds available", 6, 5, false},
		{"zero", 0, 5, false},
		{"negative", -1, 5, false},
		{"no stock", 1, 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := validSaleQuantity(tt.sold, tt.in); got != tt.wantValid {
				t.Errorf("validSaleQuantity(%d, %d) = %t, want %t", tt.sold, tt.in, got, tt.wantValid)
			}
		})
	}
}

func TestNormalizeCustomerPhone(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"international format", "+380 (67) 123-45-67", "0671234567", false},
		{"international with spaced country code", "+ 38 097 323 24 57", "0973232457", false},
		{"local format", "067 123 45 67", "0671234567", false},
		{"international format without plus", "380 96 123 45 67", "0961234567", false},
		{"optional empty", "   ", "", false},
		{"too short", "096 123 45", "", true},
		{"too long", "097 323 245 75", "", true},
		{"missing Ukrainian prefix", "+1 202 555 0142", "", true},
		{"plus not at beginning", "096+1234567", "", true},
		{"letters rejected", "067ABC1234567", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeCustomerPhone(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("normalizeCustomerPhone(%q) error = %v, wantErr %t", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("normalizeCustomerPhone(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSalesTemplateRendersWarrantyAndPhoneSearchData(t *testing.T) {
	recorder := httptest.NewRecorder()
	data := PageContext{
		CurrentUser: "Менеджер",
		Role:        "manager",
		Data: SalesPageData{
			Products: []Product{{ID: 7, Title: "Test phone", SKU: "TEST-007", Quantity: 2, WarrantyMonth: 24}},
			Sales:    []Sale{{ID: 11, ProductTitle: "Test phone", CustomerPhone: "380671234567", QuantitySold: 1}},
		},
	}
	renderTemplate(recorder, "sales.html", data)
	if recorder.Code != http.StatusOK {
		t.Fatalf("sales template response status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	for _, expected := range []string{`name="customer_phone"`, `data-warranty-months="24"`, `data-sale-phone="380671234567"`, "гарантійного звернення"} {
		if !strings.Contains(body, expected) {
			t.Errorf("sales template does not contain %q", expected)
		}
	}
}

func TestVerifyPassword(t *testing.T) {
	encoded := hashPassword("correct horse battery staple")
	if !verifyPassword("correct horse battery staple", encoded) {
		t.Fatal("hashed password was not accepted")
	}
	if verifyPassword("wrong password", encoded) {
		t.Fatal("wrong password was accepted")
	}
	legacy := sha256.Sum256([]byte("legacy-password"))
	if !verifyPassword("legacy-password", hex.EncodeToString(legacy[:])) {
		t.Fatal("legacy SHA-256 password was not accepted")
	}
}

func TestParseProductForm(t *testing.T) {
	t.Chdir(t.TempDir())
	fields := map[string]string{
		"title": "Test phone", "sku": "TEST-001", "category_id": "0", "brand_id": "0",
		"purchase_price": "1.15", "selling_price": "12.99", "quantity": "3", "warranty_months": "24",
	}
	imageData, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aWl0AAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	req := makeProductRequest(t, fields, "phone.png", imageData)
	product, err := parseProductForm(httptest.NewRecorder(), req, "")
	if err != nil {
		t.Fatalf("parseProductForm returned error: %v", err)
	}
	if product.Title != "Test phone" || product.SKU != "TEST-001" || product.Quantity != 3 || product.WarrantyMonth != 24 {
		t.Fatalf("unexpected parsed product: %+v", product)
	}
	if product.PurchasePrice != 1.15 || product.SellingPrice != 12.99 {
		t.Fatalf("unexpected parsed prices: purchase=%v selling=%v", product.PurchasePrice, product.SellingPrice)
	}
	if filepath.Ext(product.ImagePath) != ".png" || !strings.HasPrefix(product.ImagePath, "/uploads/") {
		t.Fatalf("unexpected uploaded image path: %q", product.ImagePath)
	}
	saved, err := os.ReadFile(filepath.Join("uploads", filepath.Base(product.ImagePath)))
	if err != nil || !bytes.Equal(saved, imageData) {
		t.Fatalf("uploaded image was not saved intact (err=%v)", err)
	}
}

func TestParseProductFormRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name   string
		change func(map[string]string)
	}{
		{"empty title", func(f map[string]string) { f["title"] = " " }},
		{"negative quantity", func(f map[string]string) { f["quantity"] = "-1" }},
		{"negative price", func(f map[string]string) { f["selling_price"] = "-0.01" }},
		{"NaN price", func(f map[string]string) { f["purchase_price"] = "NaN" }},
		{"infinite price", func(f map[string]string) { f["selling_price"] = "+Inf" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := validProductFields()
			tt.change(fields)
			if _, err := parseProductForm(httptest.NewRecorder(), makeProductRequest(t, fields, "", nil), ""); err == nil {
				t.Fatal("expected invalid product form to be rejected")
			}
		})
	}
}

func TestParseProductFormRejectsInvalidImage(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := parseProductForm(httptest.NewRecorder(), makeProductRequest(t, validProductFields(), "not-image.png", []byte("not an image")), ""); err == nil {
		t.Fatal("expected invalid image to be rejected")
	}
}

func TestSaveProductImageUsesSupabaseStorage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/storage/v1/object/product-images/photo.png" {
			t.Errorf("unexpected upload request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" || r.Header.Get("apikey") != "test-secret" {
			t.Error("Supabase authorization headers were not set")
		}
		if r.Header.Get("Content-Type") != "image/png" {
			t.Errorf("unexpected content type: %q", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	t.Setenv("APP_ENV", "production")
	t.Setenv("SUPABASE_URL", server.URL)
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "test-secret")
	t.Setenv("SUPABASE_STORAGE_BUCKET", "product-images")

	path, err := saveProductImage("photo.png", []byte("png-data"), "image/png")
	if err != nil {
		t.Fatalf("saveProductImage returned error: %v", err)
	}
	want := server.URL + "/storage/v1/object/public/product-images/photo.png"
	if path != want {
		t.Errorf("saveProductImage path = %q, want %q", path, want)
	}
}

func TestSaveProductImageRequiresRemoteStorageInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "")
	if _, err := saveProductImage("photo.png", []byte("png-data"), "image/png"); err == nil {
		t.Fatal("expected production image upload without persistent storage to fail")
	}
}

func TestRemoveProductImageDeletesSupabaseObject(t *testing.T) {
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deleted = true
		if r.Method != http.MethodDelete || r.URL.Path != "/storage/v1/object/product-images" {
			t.Errorf("unexpected delete request: %s %s", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Contains(body, []byte(`"photo.png"`)) {
			t.Errorf("unexpected delete payload %q (err=%v)", body, err)
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("Supabase authorization header was not set")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	t.Setenv("SUPABASE_URL", server.URL)
	t.Setenv("SUPABASE_SERVICE_ROLE_KEY", "test-secret")
	t.Setenv("SUPABASE_STORAGE_BUCKET", "product-images")

	removeProductImage(server.URL + "/storage/v1/object/public/product-images/photo.png")
	if !deleted {
		t.Fatal("expected Supabase image deletion request")
	}
}

func validProductFields() map[string]string {
	return map[string]string{
		"title": "Test phone", "sku": "TEST-001", "category_id": "0", "brand_id": "0",
		"purchase_price": "1.15", "selling_price": "12.99", "quantity": "3", "warranty_months": "24",
	}
}

func makeProductRequest(t *testing.T, fields map[string]string, fileName string, fileData []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if fileName != "" {
		part, err := writer.CreateFormFile("image", fileName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(fileData); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/products/add", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func ExampleformatMoney() {
	fmt.Println(formatMoney(1137459.50))
	// Output: 1 137 459.50 ₴
}
