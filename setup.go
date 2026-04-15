package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const statusLineCommand = "ccbar"

func runSetup() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("✗ Could not determine home directory.")
		os.Exit(1)
	}

	settingsPath := filepath.Join(home, ".claude", "settings.json")

	fmt.Println()
	fmt.Println("  ccbar setup")
	fmt.Println("  ───────────")
	fmt.Println()

	// Step 1: Read existing settings
	var settings map[string]interface{}

	if data, err := os.ReadFile(settingsPath); err == nil {
		if json.Unmarshal(data, &settings) != nil {
			settings = make(map[string]interface{})
		}
	} else {
		settings = make(map[string]interface{})
		// Ensure directory exists
		_ = os.MkdirAll(filepath.Dir(settingsPath), 0755)
	}

	// Step 2: Check if already configured
	if existing, ok := settings["statusLine"]; ok {
		if sl, ok := existing.(map[string]interface{}); ok {
			if cmd, ok := sl["command"].(string); ok && strings.Contains(cmd, "ccbar") {
				fmt.Println("  ✓ ccbar is already configured in your Claude Code settings.")
				fmt.Printf("    %s\n", settingsPath)
				fmt.Println()
				return
			}
		}
	}

	// Step 3: Show what will change
	fmt.Printf("  This will add the following to %s:\n", settingsPath)
	fmt.Println()
	fmt.Println("    \"statusLine\": {")
	fmt.Println("      \"type\": \"command\",")
	fmt.Printf("      \"command\": \"%s\",\n", statusLineCommand)
	fmt.Println("      \"refreshInterval\": 3")
	fmt.Println("    }")
	fmt.Println()

	if existing, ok := settings["statusLine"]; ok {
		existingJSON, _ := json.MarshalIndent(existing, "    ", "  ")
		fmt.Println("  ⚠ This will replace your current statusLine config:")
		fmt.Printf("    %s\n", string(existingJSON))
		fmt.Println()
	}

	// Step 4: Ask for confirmation
	fmt.Print("  Proceed? [Y/n] ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))

	if answer != "" && answer != "y" && answer != "yes" {
		fmt.Println()
		fmt.Println("  Cancelled. No changes were made.")
		fmt.Println()
		return
	}

	// Step 5: Write settings
	settings["statusLine"] = map[string]interface{}{
		"type":            "command",
		"command":         statusLineCommand,
		"refreshInterval": 3,
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		fmt.Println("  ✗ Failed to serialize settings.")
		os.Exit(1)
	}

	if err := os.WriteFile(settingsPath, append(data, '\n'), 0644); err != nil {
		fmt.Printf("  ✗ Failed to write %s: %v\n", settingsPath, err)
		os.Exit(1)
	}

	// Step 6: Success message
	fmt.Println()
	fmt.Println("  ✓ Done! ccbar has been added to your Claude Code settings.")
	fmt.Println()
	fmt.Println("  Restart Claude Code to see the status line.")
	fmt.Println()
}
