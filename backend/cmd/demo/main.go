// First-run demo preparation. Existing days and credentials are retained.
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"magistral/backend/internal/application"
	"magistral/backend/internal/auth"
	"magistral/backend/internal/dataset"
	"magistral/backend/internal/dto"
	"magistral/backend/internal/optimization"
	"magistral/backend/internal/storage/postgres"
	"os"
	"strings"
	"time"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, e := postgres.Open(ctx, os.Getenv("DATABASE_URL"))
	if e != nil {
		return e
	}
	defer store.Pool.Close()
	date := os.Getenv("DEMO_DATE")
	if date == "" {
		date = "2026-11-16"
	}
	service := &application.Service{Store: store}
	if target := os.Getenv("OPTIMIZER_URL"); target != "" {
		service.Optimizer, e = optimization.New(target)
		if e != nil {
			return e
		}
	}
	var loaded bool
	if e = store.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM reference_data)").Scan(&loaded); e != nil {
		return e
	}
	if !loaded {
		data, e := loadDemo("/app/data/synthetic", date)
		if e != nil {
			return e
		}
		if e = service.Reference(ctx, data.Reference); e != nil {
			return e
		}
		if e = service.Matrix(ctx, data.Matrix); e != nil {
			return e
		}
		if e = service.Day(ctx, date, data.Day); e != nil {
			return e
		}
	}
	// Recover interrupted first setup without replacing any existing day.
	var hasDay bool
	if e = store.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM days WHERE date::text=$1)", date).Scan(&hasDay); e != nil {
		return e
	}
	if !hasDay {
		return fmt.Errorf("demo date %s is absent in an existing database; refusing to replace data", date)
	}
	workspace, e := store.Workspace(ctx, date)
	if e != nil {
		return e
	}
	if workspace.Plan == nil {
		if _, e = service.BuildPlan(ctx, date, 30); e != nil {
			return e
		}
	}
	const file = "/demo/demo-accounts.json"
	var accounts []auth.NewUser
	raw, e := os.ReadFile(file)
	if e == nil {
		if e = json.Unmarshal(raw, &accounts); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if len(accounts) == 0 {
		accounts = []auth.NewUser{{Username: "dispatcher", Name: "Диспетчер МаксТелеком", Role: auth.Dispatcher}, {Username: "subscriber", Name: "Демонстрационный абонент", Role: auth.Subscriber}}
		for _, p := range workspace.Reference.Staff {
			name := p.StaffId
			if p.DisplayName != nil {
				name = *p.DisplayName
			}
			username := "demo." + strings.ToLower(p.StaffId)
			if p.StaffId == "ST-001" {
				username = "technician"
			}
			accounts = append(accounts, auth.NewUser{Username: username, Name: name, Role: auth.Technician, StaffID: p.StaffId})
		}
		for i := range accounts {
			var secret [18]byte
			if _, e = rand.Read(secret[:]); e != nil {
				return e
			}
			accounts[i].Password = "Mt!" + base64.RawURLEncoding.EncodeToString(secret[:])
		}
		raw, e = json.MarshalIndent(accounts, "", "  ")
		if e != nil {
			return e
		}
		if e = os.WriteFile(file, raw, 0600); e != nil {
			return e
		}
	}
	manager := &auth.Manager{Store: store}
	for _, account := range accounts {
		var exists bool
		if e = store.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM app_users WHERE username=$1)", strings.ToLower(account.Username)).Scan(&exists); e != nil {
			return e
		}
		if !exists {
			if _, e = manager.Create(ctx, account); e != nil {
				return e
			}
		}
	}
	fmt.Printf("Demo ready: %s, %d orders, %d employees. Accounts: /demo/demo-accounts.json (existing passwords retained).\n", date, len(workspace.Day.Orders), len(workspace.Reference.Staff))
	return nil
}

func loadDemo(dir, date string) (dataset.Data, error) {
	var manifest struct {
		Date string `json:"date"`
	}
	raw, e := os.ReadFile(dir + "/manifest.json")
	if e != nil {
		return dataset.Data{}, e
	}
	if e = json.Unmarshal(raw, &manifest); e != nil {
		return dataset.Data{}, e
	}
	data, e := dataset.Load(dir, manifest.Date, manifest.Date+"T08:45:00+05:00")
	if e != nil {
		return data, e
	}
	from, e := time.Parse("2006-01-02", manifest.Date)
	if e != nil {
		return data, e
	}
	to, e := time.Parse("2006-01-02", date)
	if e != nil {
		return data, e
	}
	offset := int(to.Sub(from).Hours() / 24)
	shift := func(v dto.DateTime) dto.DateTime {
		parsed, _ := time.Parse(time.RFC3339, string(v))
		return dto.DateTime(parsed.AddDate(0, 0, offset).Format(time.RFC3339))
	}
	id := func(v string) string {
		return strings.ReplaceAll(v, strings.ReplaceAll(manifest.Date, "-", ""), strings.ReplaceAll(date, "-", ""))
	}
	data.Day.Date = date
	data.Day.Now = shift(data.Day.Now)
	for i := range data.Day.Orders {
		o := &data.Day.Orders[i]
		o.OrderId = id(o.OrderId)
		o.RegisteredAt = shift(o.RegisteredAt)
		o.Slot.Start = shift(o.Slot.Start)
		o.Slot.End = shift(o.Slot.End)
		o.LatestStart = shift(o.LatestStart)
		o.Deadline = shift(o.Deadline)
		if o.PreviousOrderId != nil {
			previous := id(*o.PreviousOrderId)
			o.PreviousOrderId = &previous
		}
	}
	if len(data.Day.Orders) != 400 || len(data.Reference.Staff) != 40 {
		return data, fmt.Errorf("expected complete 40/400 synthetic data")
	}
	return data, application.ValidateDay(date, &data.Day, data.Reference)
}
