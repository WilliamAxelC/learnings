package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"
)

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func main() {
	swarmMode := os.Getenv("RB_SWARM_MODE") == "true"
	for _, arg := range os.Args {
		if arg == "-swarm" || arg == "--swarm" {
			swarmMode = true
		}
	}

	if swarmMode {
		// Multi-bot swarm mode: tuned for concurrent bots and HTTP server
		runtime.GOMAXPROCS(2)
		debug.SetGCPercent(50)
		debug.SetMemoryLimit(128 * 1024 * 1024)
	} else {
		// Ultra-lean single-bot mode (Cuang / William): < 6 MiB RSS
		runtime.GOMAXPROCS(1)
		debug.SetGCPercent(50)
		debug.SetMemoryLimit(16 * 1024 * 1024)
	}

	// Fetch game defs with retries
	var defs *GameDefs
	var err error
	for i := 0; i < 5; i++ {
		defs, err = FetchGameDefs()
		if err == nil {
			break
		}
		fmt.Printf("[WARN] Failed to fetch game defs (attempt %d/5): %v. Retrying in 2s...\n", i+1, err)
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		fmt.Printf("[FATAL] Could not fetch game definitions: %v\n", err)
		os.Exit(1)
	}

	if swarmMode {
		configPath := getEnv("RB_SWARM_CONFIG", "./fleet.json")
		portStr := getEnv("RB_SWARM_PORT", "8989")
		port := 8989
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}

		fmt.Printf("[SWARM] === Reefblast Autonomous Go Swarm Engine Starting (Port %d, Config: %s) ===\n", port, configPath)
		sm := NewSwarmManager(defs, configPath, port)
		if err := sm.Start(); err != nil {
			fmt.Printf("[FATAL] Swarm engine start failed: %v\n", err)
			os.Exit(1)
		}

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		sig := <-sigChan
		fmt.Printf("[SWARM] Received shutdown signal (%v). Terminating swarm cleanly...\n", sig)
		sm.StopAll()
		return
	}

	botName := getEnv("RB_BOT_NAME", "cuang-bot")
	botPass := getEnv("RB_BOT_PASS", "Rb!dIkC3VsYPED15")
	token := getEnv("RB_TOKEN", "")

	fmt.Printf("[SYSTEM] Starting Reefblast Bot Go Daemon for '%s'...\n", botName)
	daemon := NewReefBlastDaemon(defs, botName, botPass, token)
	daemon.Start()

	// Wait for termination signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	sig := <-sigChan
	daemon.Log("SYSTEM", "Received shutdown signal (%v). Terminating gracefully...", sig)
	daemon.Stop()
}
