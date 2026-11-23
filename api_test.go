package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func makeArchive(t *testing.T, kind, text string) []byte {
	t.Helper()
	var b bytes.Buffer
	if kind == "zip" {
		z := zip.NewWriter(&b)
		f, e := z.Create("sample_data/data.csv")
		if e != nil {
			t.Fatal(e)
		}
		io.WriteString(f, text)
		if e = z.Close(); e != nil {
			t.Fatal(e)
		}
	} else {
		z := tar.NewWriter(&b)
		if e := z.WriteHeader(&tar.Header{Name: "test_data.csv", Mode: 0600, Size: int64(len(text))}); e != nil {
			t.Fatal(e)
		}
		io.WriteString(z, text)
		if e := z.Close(); e != nil {
			t.Fatal(e)
		}
	}
	return b.Bytes()
}
func TestArchiveAndCSV(t *testing.T) {
	const input = "id,name,category,price,create_date\n1,\"Кофе, зерно\",Напитки,12.34,2024-02-29\n"
	for _, kind := range []string{"zip", "tar"} {
		data, e := extractCSV(makeArchive(t, kind, input), kind)
		if e != nil {
			t.Fatal(e)
		}
		items, e := parseCSV(data)
		if e != nil || len(items) != 1 || items[0].Price != "12.34" || items[0].Name != "Кофе, зерно" {
			t.Fatalf("parse: %+v %v", items, e)
		}
	}
	for _, s := range []string{"-1", "NaN", "1e3", "1.001", "", "99999999999999999"} {
		if validPrice(s) {
			t.Fatalf("accepted invalid price %q", s)
		}
	}
	for _, s := range []string{"0", "12", "9999999999999999.99"} {
		if !validPrice(s) {
			t.Fatalf("rejected price %q", s)
		}
	}
	for _, input := range []string{"", "id,id,name,category,price,create_date\n", "id,name,category,price\n", "id,name,category,price,create_date\n1,x,c,1,2023-02-29\n"} {
		if _, e := parseCSV([]byte(input)); e == nil {
			t.Fatalf("accepted invalid CSV: %q", input)
		}
	}
	if _, e := extractCSV([]byte("broken"), "zip"); e == nil {
		t.Fatal("accepted invalid ZIP")
	}
	if _, e := limitedRead(bytes.NewBufferString("abcd"), 3); e == nil {
		t.Fatal("ignored size limit")
	}
}
func TestAPIWithPostgreSQL(t *testing.T) {
	if os.Getenv("TEST_POSTGRES") != "1" {
		t.Skip("set TEST_POSTGRES=1 for isolated-schema integration test")
	}
	db, e := connect()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	namespace := fmt.Sprintf("test_prices_%d", time.Now().UnixNano())
	if _, e = db.Exec("CREATE SCHEMA " + namespace); e != nil {
		t.Fatal(e)
	}
	defer db.Exec("DROP SCHEMA " + namespace + " CASCADE")
	if _, e = db.Exec("SET search_path TO " + namespace); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(schema); e != nil {
		t.Fatal(e)
	}
	a := &API{db}
	ctx := context.Background()
	const csvText = "id,name,category,price,create_date\n1,\"Кофе, зерно\",food,12.34,2024-01-01\n2,Tea,food,1.01,2024-01-02\n"
	upload := func(kind string, multi bool, text string) Summary {
		t.Helper()
		data := makeArchive(t, kind, text)
		contentType := "application/octet-stream"
		if multi {
			var b bytes.Buffer
			w := multipart.NewWriter(&b)
			f, err := w.CreateFormFile("file", "input."+kind)
			if err != nil {
				t.Fatal(err)
			}
			f.Write(data)
			w.Close()
			data = b.Bytes()
			contentType = w.FormDataContentType()
		}
		r := httptest.NewRequest("POST", "/api/v0/prices?type="+kind, bytes.NewReader(data))
		r.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		a.prices(w, r)
		if w.Code != 200 {
			t.Fatalf("POST %s: %d %s", kind, w.Code, w.Body.String())
		}
		var result Summary
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := upload("zip", false, csvText)
	if first.Items != 2 || first.Categories != 1 || first.Price != "13.35" {
		t.Fatalf("summary: %+v", first)
	}
	second := upload("tar", true, csvText)
	if second.Items != 2 || second.Price != "26.70" {
		t.Fatalf("repeat: %+v", second)
	}
	third := upload("zip", true, "id,name,category,price,create_date\n3,Soap,house,2.00,2024-01-03\n")
	if third.Categories != 2 || third.Price != "28.70" {
		t.Fatalf("global summary: %+v", third)
	}
	w := httptest.NewRecorder()
	a.prices(w, httptest.NewRequest("GET", "/api/v0/prices", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil || len(z.File) != 1 || z.File[0].Name != "data.csv" {
		t.Fatalf("export ZIP: %v", err)
	}
	f, err := z.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(f).ReadAll()
	f.Close()
	if err != nil || len(records) != 6 || records[1][1] != "Кофе, зерно" || records[3][0] != "1" {
		t.Fatalf("round trip: %v %v", records, err)
	}
	w = httptest.NewRecorder()
	a.prices(w, httptest.NewRequest("POST", "/api/v0/prices", bytes.NewReader(makeArchive(t, "zip", csvText+"9,x,c,bad,2024-01-01\n"))))
	if w.Code != 400 {
		t.Fatal("expected invalid CSV rejection", w.Code)
	}
	var count int
	if err = db.QueryRowContext(ctx, "SELECT count(*) FROM prices").Scan(&count); err != nil || count != 5 {
		t.Fatalf("partial insert: %d %v", count, err)
	}
	for _, kind := range []string{"rar", "unknown"} {
		w = httptest.NewRecorder()
		a.prices(w, httptest.NewRequest("POST", "/api/v0/prices?type="+kind, nil))
		if w.Code != 400 {
			t.Fatal("expected bad type")
		}
	}
	w = httptest.NewRecorder()
	a.prices(w, httptest.NewRequest("DELETE", "/api/v0/prices", nil))
	if w.Code != 405 {
		t.Fatal("expected method not allowed")
	}
}
