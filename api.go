package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lib/pq"
	"io"
	"log"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

const maxArchive = 32 << 20
const maxCSV = 128 << 20

type API struct{ db *sql.DB }
type Item struct {
	ID                    int64
	Name, Category, Price string
	Date                  time.Time
}
type Summary struct {
	Items      int64       `json:"total_items"`
	Categories int64       `json:"total_categories"`
	Price      json.Number `json:"total_price"`
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.db.PingContext(ctx); err != nil {
		http.Error(w, "database unavailable", 503)
		return
	}
	w.WriteHeader(200)
	io.WriteString(w, "ok\n")
}
func (a *API) prices(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/v0/prices" {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPost:
		a.upload(w, r)
	case http.MethodGet:
		a.download(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
	}
}
func limitedRead(reader io.Reader, limit int64) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(reader, limit+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	return b, nil
}
func archiveBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxArchive+(1<<20))
	defer r.Body.Close()
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil && r.Header.Get("Content-Type") != "" {
		return nil, errors.New("invalid Content-Type")
	}
	if media == "multipart/form-data" {
		reader, e := r.MultipartReader()
		if e != nil {
			return nil, e
		}
		var data []byte
		found := false
		for {
			part, e := reader.NextPart()
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, e
			}
			if part.FormName() == "file" && part.FileName() != "" {
				if found {
					part.Close()
					return nil, errors.New("only one file is allowed")
				}
				data, e = limitedRead(part, maxArchive)
				found = true
			}
			part.Close()
			if e != nil {
				return nil, e
			}
		}
		if !found {
			return nil, errors.New("multipart field file is required")
		}
		return data, nil
	}
	return limitedRead(r.Body, maxArchive)
}
func extractCSV(data []byte, kind string) ([]byte, error) {
	var result []byte
	found := false
	accept := func(name string, reader io.Reader) error {
		if !strings.EqualFold(path.Ext(name), ".csv") || strings.HasPrefix(path.Base(name), "._") {
			return nil
		}
		if found {
			return errors.New("archive must contain exactly one CSV file")
		}
		b, e := limitedRead(reader, maxCSV)
		if e != nil {
			return e
		}
		found = true
		result = b
		return nil
	}
	switch kind {
	case "zip":
		z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if e != nil {
			return nil, errors.New("invalid zip archive")
		}
		for _, f := range z.File {
			if f.FileInfo().IsDir() {
				continue
			}
			if f.UncompressedSize64 > maxCSV {
				return nil, errors.New("uncompressed file is too large")
			}
			reader, e := f.Open()
			if e != nil {
				return nil, e
			}
			e = accept(f.Name, reader)
			reader.Close()
			if e != nil {
				return nil, e
			}
		}
	case "tar":
		t := tar.NewReader(bytes.NewReader(data))
		for {
			h, e := t.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, errors.New("invalid tar archive")
			}
			if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
				continue
			}
			if e = accept(h.Name, t); e != nil {
				return nil, e
			}
		}
	default:
		return nil, errors.New("type must be zip or tar")
	}
	if !found {
		return nil, errors.New("CSV file not found")
	}
	return result, nil
}
func parseCSV(data []byte) ([]Item, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	header, err := reader.Read()
	if err != nil {
		return nil, errors.New("CSV header is missing")
	}
	reader.FieldsPerRecord = len(header)
	columns := map[string]int{}
	for i, name := range header {
		name = strings.TrimPrefix(name, "\ufeff")
		if _, ok := columns[name]; ok {
			return nil, errors.New("duplicate CSV column")
		}
		columns[name] = i
	}
	for _, name := range []string{"id", "name", "category", "price", "create_date"} {
		if _, ok := columns[name]; !ok {
			return nil, fmt.Errorf("missing CSV column: %s", name)
		}
	}
	var items []Item
	for line := 2; ; line++ {
		row, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, fmt.Errorf("CSV line %d: %w", line, e)
		}
		get := func(k string) string { return row[columns[k]] }
		id, e := strconv.ParseInt(get("id"), 10, 64)
		if e != nil || id <= 0 {
			return nil, fmt.Errorf("CSV line %d: invalid id", line)
		}
		date, e := time.Parse("2006-01-02", get("create_date"))
		if e != nil {
			return nil, fmt.Errorf("CSV line %d: invalid date", line)
		}
		price := get("price")
		if !validPrice(price) {
			return nil, fmt.Errorf("CSV line %d: invalid price", line)
		}
		if strings.TrimSpace(get("name")) == "" || strings.TrimSpace(get("category")) == "" {
			return nil, fmt.Errorf("CSV line %d: missing name or category", line)
		}
		items = append(items, Item{id, get("name"), get("category"), price, date})
	}
	return items, nil
}
func validPrice(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) > 2 || parts[0] == "" || len(parts[0]) > 16 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return len(parts) == 1 || len(parts[1]) <= 2
}
func (a *API) upload(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("type")
	if kind == "" {
		kind = "zip"
	}
	if kind != "zip" && kind != "tar" {
		http.Error(w, "type must be zip or tar", 400)
		return
	}
	data, err := archiveBody(w, r)
	if err != nil {
		status := 400
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			status = 413
		}
		http.Error(w, err.Error(), status)
		return
	}
	data, err = extractCSV(data, kind)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	items, err := parseCSV(data)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	summary, err := a.insert(ctx, items)
	if err != nil {
		log.Printf("upload database error: %v", err)
		http.Error(w, "database operation failed", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}
func (a *API) insert(ctx context.Context, items []Item) (Summary, error) {
	result := Summary{Items: int64(len(items))}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, pq.CopyIn("prices", "id", "name", "category", "price", "create_date"))
	if err != nil {
		return result, err
	}
	defer stmt.Close()
	for _, v := range items {
		if _, err = stmt.ExecContext(ctx, v.ID, v.Name, v.Category, v.Price, v.Date); err != nil {
			return result, err
		}
	}
	if _, err = stmt.ExecContext(ctx); err != nil {
		return result, err
	}
	if err = stmt.Close(); err != nil {
		return result, err
	}
	var total string
	err = tx.QueryRowContext(ctx, `SELECT count(DISTINCT category), coalesce(sum(price),0)::text FROM prices`).Scan(&result.Categories, &total)
	if err != nil {
		return result, err
	}
	result.Price = json.Number(total)
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}
func (a *API) download(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	rows, err := a.db.QueryContext(ctx, `SELECT id,name,category,price::text,create_date FROM prices ORDER BY row_id`)
	if err != nil {
		log.Printf("export query: %v", err)
		http.Error(w, "database operation failed", 500)
		return
	}
	defer rows.Close()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	entry, err := z.Create("data.csv")
	if err != nil {
		http.Error(w, "cannot create archive", 500)
		return
	}
	writer := csv.NewWriter(entry)
	err = writer.Write([]string{"id", "name", "category", "price", "create_date"})
	for err == nil && rows.Next() {
		var v Item
		if err = rows.Scan(&v.ID, &v.Name, &v.Category, &v.Price, &v.Date); err == nil {
			err = writer.Write([]string{strconv.FormatInt(v.ID, 10), v.Name, v.Category, v.Price, v.Date.Format("2006-01-02")})
		}
	}
	if err == nil {
		err = rows.Err()
	}
	writer.Flush()
	if err == nil {
		err = writer.Error()
	}
	if e := z.Close(); err == nil {
		err = e
	}
	if err != nil {
		log.Printf("export: %v", err)
		http.Error(w, "export failed", 500)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="prices.zip"`)
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.Write(buf.Bytes())
}
