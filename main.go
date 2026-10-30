package main

import (
	"context"
	"embed"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	gradingdata "github.com/aexygit/glimpse/data/grading"
	"github.com/aexygit/glimpse/internal/app"
	"github.com/aexygit/glimpse/internal/capture"
	"github.com/aexygit/glimpse/internal/gamedata"
	"github.com/aexygit/glimpse/internal/grading"
	"github.com/aexygit/glimpse/internal/protocol"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfgBase, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	cacheBase, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	cfgDir, cacheDir := filepath.Join(cfgBase, "glimpse"), filepath.Join(cacheBase, "glimpse")

	if logger, closer, err := app.SetupLog(cfgDir); err != nil {
		slog.Error("log setup failed, logging to stderr", "error", err)
	} else {
		defer closer.Close()
		slog.SetDefault(logger)
	}

	store, err := gamedata.Open(cacheDir)
	if err != nil {
		return err
	}
	fetcher, err := gamedata.NewFetcher(store, nil)
	if err != nil {
		return err
	}
	rules, ruleErrs := grading.LoadRules(gradingdata.FS, filepath.Join(cfgDir, "grading"))

	var wapp *application.App
	emit := func(name string, data any) { wapp.Event.Emit(name, data) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := app.New(ctx, store, fetcher, rules, map[string]string{
		"cache": cacheDir, "config": cfgDir, "log": filepath.Join(cfgDir, "glimpse.log"),
	}, emit)

	wapp = application.New(application.Options{
		Name:        "Glimpse",
		Description: "Aion 2 gear inspector",
		Services:    []application.Service{application.NewService(svc)},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: svc.IconHandler,
		},
		OnShutdown: cancel,
	})
	for _, e := range ruleErrs {
		slog.Warn("rules", "error", e)
		svc.AddWarning(e.Error())
	}

	go func() {
		var err error
		if p := os.Getenv("GLIMPSE_REPLAY"); p != "" {
			err = capture.Replay(ctx, p, svc.OnMessage)
		} else {
			err = (&capture.Capturer{OnStatus: svc.SetStatus, OnMessage: svc.OnMessage}).Run(ctx)
		}
		if err != nil {
			slog.Error("capture", "error", err)
		}
	}()
	if os.Getenv("GLIMPSE_FAKE") == "1" {
		svc.OnMessage(fakeInspect())
	}

	wapp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Glimpse",
		Width:  1000,
		Height: 618,
		URL:    "/",
	})
	return wapp.Run()
}

func fakeInspect() protocol.InspectResult {
	return protocol.InspectResult{Name: "Fake", Class: "Assassin", Level: 65, Items: []protocol.InspectItem{
		{Slot: "MainHand", ID: 123, Enchant: 10},
		{Slot: "Ring1", ID: 456, Enchant: 5},
	}}
}
