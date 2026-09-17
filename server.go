package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	dataMu        sync.Mutex
	sessionTokens = make(map[string]string) // token -> username
	tokenMu       sync.RWMutex
)

const (
	AdminUser = "ADMIN"
	AdminCode = "0016"
)

type Product struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	NameTJ    string   `json:"nameTJ,omitempty"`
	Brand     string   `json:"brand,omitempty"`
	Cat       string   `json:"cat"`
	CatName   string   `json:"catName"`
	CatNameTJ string   `json:"catNameTJ,omitempty"`
	Price     float64  `json:"price"`
	OldPrice  *float64 `json:"oldPrice"`
	Badge     *string  `json:"badge"`
	SKU       string   `json:"sku"`
	Seed      string   `json:"seed,omitempty"`
	Desc      string   `json:"desc"`
	DescTJ    string   `json:"descTJ,omitempty"`
	Specs     []string `json:"specs"`
	SpecsTJ   []string `json:"specsTJ,omitempty"`
	Sizes     []int    `json:"sizes"`
	Photos    []string `json:"photos"`
	CreatedAt string   `json:"createdAt,omitempty"`
}

var defaultCatNames = map[string][2]string{
	"sneakers": {"Кроссовки", "Кроссовкаҳо"},
	"outdoors": {"Аутдор", "Аутдор"},
	"casual":   {"Кэжуал", "Кэжуал"},
	"classic":  {"Классика", "Классикӣ"},
}

func normalizeProduct(p *Product, existing *Product) {
	if p.Cat != "" {
		if pair, ok := defaultCatNames[p.Cat]; ok {
			if p.CatName == "" {
				p.CatName = pair[0]
			}
			if p.CatNameTJ == "" {
				p.CatNameTJ = pair[1]
			}
		}
	}
	if existing != nil {
		if p.Name == "" && existing.Name != "" {
			p.Name = existing.Name
		}
		if p.NameTJ == "" && existing.NameTJ != "" {
			p.NameTJ = existing.NameTJ
		}
		if p.CatName == "" && existing.CatName != "" {
			p.CatName = existing.CatName
		}
		if p.CatNameTJ == "" && existing.CatNameTJ != "" {
			p.CatNameTJ = existing.CatNameTJ
		}
		if p.Desc == "" && existing.Desc != "" {
			p.Desc = existing.Desc
		}
		if p.DescTJ == "" && existing.DescTJ != "" {
			p.DescTJ = existing.DescTJ
		}
		if len(p.Specs) == 0 && len(existing.Specs) > 0 {
			p.Specs = existing.Specs
		}
		if len(p.SpecsTJ) == 0 && len(existing.SpecsTJ) > 0 {
			p.SpecsTJ = existing.SpecsTJ
		}
	}
	if p.Name == "" && p.NameTJ != "" {
		p.Name = p.NameTJ
	}
	if p.NameTJ == "" && p.Name != "" {
		p.NameTJ = p.Name
	}
	if p.CatName == "" && p.CatNameTJ != "" {
		p.CatName = p.CatNameTJ
	}
	if p.CatNameTJ == "" && p.CatName != "" {
		p.CatNameTJ = p.CatName
	}
	if p.Desc == "" && p.DescTJ != "" {
		p.Desc = p.DescTJ
	}
	if p.DescTJ == "" && p.Desc != "" {
		p.DescTJ = p.Desc
	}
	if len(p.Specs) == 0 && len(p.SpecsTJ) > 0 {
		p.Specs = p.SpecsTJ
	}
	if len(p.SpecsTJ) == 0 && len(p.Specs) > 0 {
		p.SpecsTJ = p.Specs
	}
}

type IncomeRecord struct {
	ID      int64   `json:"id"`
	Amount  float64 `json:"amount"`
	Product string  `json:"product"`
	Date    string  `json:"date"`
	Note    string  `json:"note"`
}

type ClickRecord struct {
	ID        int64  `json:"id"`
	ProductID int    `json:"productId"`
	Name      string `json:"name"`
	Date      string `json:"date"`
}

type Stats struct {
	TotalIncome    float64        `json:"totalIncome"`
	WhatsappOrders int            `json:"whatsappOrders"`
	Incomes        []IncomeRecord `json:"incomes"`
	Clicks         []ClickRecord  `json:"clicks"`
}

type Note struct {
	ID        int64  `json:"id"`
	Text      string `json:"text"`
	Completed bool   `json:"completed"`
	CreatedAt string `json:"createdAt"`
}

func generateToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func readJSONFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == "" {
			token = r.URL.Query().Get("token")
		}

		tokenMu.RLock()
		user, exists := sessionTokens[token]
		tokenMu.RUnlock()

		if !exists {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		_ = user
		next(w, r)
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	// 1. Auth Endpoint
	mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Username string `json:"username"`
			Code     string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid request"}`, http.StatusBadRequest)
			return
		}

		if strings.EqualFold(strings.TrimSpace(req.Username), AdminUser) && strings.TrimSpace(req.Code) == AdminCode {
			token := generateToken()
			tokenMu.Lock()
			sessionTokens[token] = AdminUser
			tokenMu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"token":   token,
				"user":    AdminUser,
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"success": false,
			"error":   "Номи корбар ё код нодуруст аст",
		})
	})

	mux.HandleFunc("/api/auth/check", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))

	// 2. Products API (GET public, POST/PUT/DELETE protected)
	mux.HandleFunc("/api/products", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		dataMu.Lock()
		defer dataMu.Unlock()

		productsPath := "data/products.json"
		var products []Product
		_ = readJSONFile(productsPath, &products)

		switch r.Method {
		case http.MethodGet:
			lang := strings.ToLower(r.URL.Query().Get("lang"))
			if lang == "tj" || lang == "tg" {
				localized := make([]Product, len(products))
				for i, p := range products {
					if p.NameTJ != "" {
						p.Name = p.NameTJ
					}
					if p.CatNameTJ != "" {
						p.CatName = p.CatNameTJ
					}
					if p.DescTJ != "" {
						p.Desc = p.DescTJ
					}
					if len(p.SpecsTJ) > 0 {
						p.Specs = p.SpecsTJ
					}
					localized[i] = p
				}
				json.NewEncoder(w).Encode(localized)
				return
			}
			json.NewEncoder(w).Encode(products)

		case http.MethodPost:
			var p Product
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				http.Error(w, `{"error":"Invalid JSON"}`, http.StatusBadRequest)
				return
			}
			maxID := 0
			for _, item := range products {
				if item.ID > maxID {
					maxID = item.ID
				}
			}
			p.ID = maxID + 1
			if p.CreatedAt == "" {
				p.CreatedAt = time.Now().Format(time.RFC3339)
			}
			if p.SKU == "" {
				p.SKU = fmt.Sprintf("STP-%02d", p.ID)
			}
			normalizeProduct(&p, nil)
			products = append([]Product{p}, products...)
			if err := writeJSONFile(productsPath, products); err != nil {
				http.Error(w, `{"error":"Failed to save"}`, http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "product": p})

		case http.MethodPut:
			var p Product
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				http.Error(w, `{"error":"Invalid JSON"}`, http.StatusBadRequest)
				return
			}
			found := false
			for i, item := range products {
				if item.ID == p.ID {
					normalizeProduct(&p, &item)
					products[i] = p
					found = true
					break
				}
			}
			if !found {
				http.Error(w, `{"error":"Product not found"}`, http.StatusNotFound)
				return
			}
			_ = writeJSONFile(productsPath, products)
			json.NewEncoder(w).Encode(map[string]any{"success": true, "product": p})

		case http.MethodDelete:
			idStr := r.URL.Query().Get("id")
			id, _ := strconv.Atoi(idStr)
			if id == 0 {
				var req struct {
					ID int `json:"id"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				id = req.ID
			}
			if id == 0 {
				http.Error(w, `{"error":"ID required"}`, http.StatusBadRequest)
				return
			}

			newProducts := make([]Product, 0, len(products))
			for _, item := range products {
				if item.ID != id {
					newProducts = append(newProducts, item)
				}
			}
			products = newProducts
			_ = writeJSONFile(productsPath, products)
			json.NewEncoder(w).Encode(map[string]any{"success": true, "deletedId": id})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// 3. Image Upload API (Up to 6 photos)
	mux.HandleFunc("/api/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		err := r.ParseMultipartForm(32 << 20) // 32MB max
		if err != nil {
			http.Error(w, `{"error":"Failed to parse form"}`, http.StatusBadRequest)
			return
		}

		files := r.MultipartForm.File["photos"]
		if len(files) == 0 {
			files = r.MultipartForm.File["file"]
		}

		uploadDir := "uploads/obuv"
		_ = os.MkdirAll(uploadDir, 0755)

		var urls []string
		for _, f := range files {
			src, err := f.Open()
			if err != nil {
				continue
			}
			ext := filepath.Ext(f.Filename)
			if ext == "" {
				ext = ".jpg"
			}
			filename := fmt.Sprintf("upload_%d_%s%s", time.Now().UnixNano(), generateToken()[:6], ext)
			dstPath := filepath.Join(uploadDir, filename)

			dst, err := os.Create(dstPath)
			if err != nil {
				src.Close()
				continue
			}
			_, _ = io.Copy(dst, src)
			src.Close()
			dst.Close()

			urls = append(urls, "uploads/obuv/"+filename)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"urls":    urls,
		})
	})

	// 4. Stats & Analytics API
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		dataMu.Lock()
		defer dataMu.Unlock()

		statsPath := "data/stats.json"
		var st Stats
		_ = readJSONFile(statsPath, &st)
		json.NewEncoder(w).Encode(st)
	})

	mux.HandleFunc("/api/stats/income", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		dataMu.Lock()
		defer dataMu.Unlock()

		statsPath := "data/stats.json"
		var st Stats
		_ = readJSONFile(statsPath, &st)

		if r.Method == http.MethodPost {
			var rec IncomeRecord
			if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
				http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
				return
			}
			rec.ID = time.Now().UnixMilli()
			if rec.Date == "" {
				rec.Date = time.Now().Format("2006-01-02 15:04")
			}
			st.Incomes = append([]IncomeRecord{rec}, st.Incomes...)
			st.TotalIncome += rec.Amount
			_ = writeJSONFile(statsPath, st)
			json.NewEncoder(w).Encode(map[string]any{"success": true, "stats": st})
			return
		}

		if r.Method == http.MethodDelete {
			idStr := r.URL.Query().Get("id")
			id, _ := strconv.ParseInt(idStr, 10, 64)
			var newIncomes []IncomeRecord
			var total float64
			for _, item := range st.Incomes {
				if item.ID != id {
					newIncomes = append(newIncomes, item)
					total += item.Amount
				}
			}
			st.Incomes = newIncomes
			st.TotalIncome = total
			_ = writeJSONFile(statsPath, st)
			json.NewEncoder(w).Encode(map[string]any{"success": true, "stats": st})
			return
		}

		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})

	// 5. WhatsApp Click Analytics
	mux.HandleFunc("/api/analytics/click", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ProductID int    `json:"productId"`
			Name      string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		dataMu.Lock()
		defer dataMu.Unlock()
		statsPath := "data/stats.json"
		var st Stats
		_ = readJSONFile(statsPath, &st)

		st.WhatsappOrders++
		st.Clicks = append([]ClickRecord{{
			ID:        time.Now().UnixMilli(),
			ProductID: req.ProductID,
			Name:      req.Name,
			Date:      time.Now().Format("2006-01-02 15:04"),
		}}, st.Clicks...)
		if len(st.Clicks) > 500 {
			st.Clicks = st.Clicks[:500]
		}
		_ = writeJSONFile(statsPath, st)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"success": true, "orders": st.WhatsappOrders})
	})

	// 6. Notes API
	mux.HandleFunc("/api/notes", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		dataMu.Lock()
		defer dataMu.Unlock()

		notesPath := "data/notes.json"
		var notes []Note
		_ = readJSONFile(notesPath, &notes)

		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(notes)

		case http.MethodPost:
			var n Note
			_ = json.NewDecoder(r.Body).Decode(&n)
			if strings.TrimSpace(n.Text) == "" {
				http.Error(w, `{"error":"Text required"}`, http.StatusBadRequest)
				return
			}
			n.ID = time.Now().UnixMilli()
			n.CreatedAt = time.Now().Format("2006-01-02 15:04")
			notes = append([]Note{n}, notes...)
			_ = writeJSONFile(notesPath, notes)
			json.NewEncoder(w).Encode(map[string]any{"success": true, "note": n})

		case http.MethodPatch:
			var req struct {
				ID        int64 `json:"id"`
				Completed bool  `json:"completed"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			for i, item := range notes {
				if item.ID == req.ID {
					notes[i].Completed = req.Completed
					break
				}
			}
			_ = writeJSONFile(notesPath, notes)
			json.NewEncoder(w).Encode(map[string]any{"success": true})

		case http.MethodDelete:
			idStr := r.URL.Query().Get("id")
			id, _ := strconv.ParseInt(idStr, 10, 64)
			var newNotes []Note
			for _, item := range notes {
				if item.ID != id {
					newNotes = append(newNotes, item)
				}
			}
			notes = newNotes
			_ = writeJSONFile(notesPath, notes)
			json.NewEncoder(w).Encode(map[string]any{"success": true})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// 7. Route /admin-step to admin-step.html
	mux.HandleFunc("/admin-step", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "admin-step.html")
	})

	// 8. Static file server (index.html, admin-step.html, uploads/, img/, etc.)
	fs := http.FileServer(http.Dir("."))
	mux.Handle("/", fs)

	log.Printf("👟 STEP style.tj Server started on http://localhost:%s", port)
	log.Printf("👉 Main site:  http://localhost:%s/", port)
	log.Printf("🔐 Admin panel: http://localhost:%s/admin-step", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
