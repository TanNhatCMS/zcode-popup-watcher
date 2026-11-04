package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	// Nhung tzdata de LoadLocation("Asia/Ho_Chi_Minh") chay duoc
	// ca tren Windows (noi khong co file zoneinfo IANA).
	_ "time/tzdata"

	"github.com/TanNhatCMS/zcode-popup-watcher/internal/checker"
)

func toMinutes(hhmm string) int {
	parts := strings.Split(hhmm, ":")
	h, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
	m := 0
	if len(parts) > 1 {
		m, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
	}
	return h*60 + m
}

func reminderStateFile() string {
	return filepath.Join(checker.StateDir(), "popup-watcher-state.json")
}

func loadNotifiedDate() string {
	data, err := os.ReadFile(reminderStateFile())
	if err != nil {
		return ""
	}
	var parsed struct {
		NotifiedDate string `json:"notifiedDate"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return ""
	}
	return parsed.NotifiedDate
}

func saveNotifiedDate(date string) {
	checker.EnsureStateDir()
	data, _ := json.MarshalIndent(map[string]string{"notifiedDate": date}, "", "  ")
	if err := os.WriteFile(reminderStateFile(), data, 0o644); err != nil {
		fmt.Printf("[watcher] khong ghi duoc state: %v\n", err)
	}
}

// vnNow tra ve ngay (YYYY-MM-DD) + so phut trong ngay theo gio VN,
// giong Intl.DateTimeFormat timeZone Asia/Ho_Chi_Minh ban Bun.
func vnNow() (string, int) {
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		loc = time.FixedZone("ICT", 7*3600)
	}
	now := time.Now().In(loc)
	return now.Format("2006-01-02"), now.Hour()*60 + now.Minute()
}

// runReminder port popup-watcher.ts: khong goi API, khong doc token.
// Den khung gio deal dem (mac dinh 17:55-22:10 gio VN) thi bao
// 1 lan/ngay. --test bao ngay khong doi khung gio.
func runReminder(args map[string]string, bools map[string]bool) int {
	fromStr := args["from"]
	if fromStr == "" {
		fromStr = "17:55"
	}
	toStr := args["to"]
	if toStr == "" {
		toStr = "22:10"
	}
	fromMinutes := toMinutes(fromStr)
	toMinutesValue := toMinutes(toStr)
	autoOpen := bools["open"]

	showReminder := func() {
		message := strings.Join([]string{
			"Toi gio nhan deal dem ZCode!",
			"",
			"1. Mo ZCode.",
			"2. Bam banner/modal deal dem trong app.",
			"3. Tu 22:00 moi dung duoc, toi 08:00 sang hom sau reset.",
			"4. Nho chon dung GLM-5.3-Flash + Global Building.",
			"",
			"Tool nay chi bao, khong tu bam nhan thay ban.",
		}, "\n")
		Notify("ZCode - Toi gio nhan deal dem!", message)
		fmt.Println(message)
		if autoOpen {
			fmt.Println(OpenZcode())
		}
	}

	if bools["test"] {
		showReminder()
		return 0
	}

	fmt.Printf("[watcher] Dang chay nen. Se bao trong khung %s-%s gio VN, 1 lan/ngay.\n", fromStr, toStr)
	for {
		date, minutes := vnNow()
		if loadNotifiedDate() != date && minutes >= fromMinutes && minutes <= toMinutesValue {
			showReminder()
			saveNotifiedDate(date)
		}
		time.Sleep(60 * time.Second)
	}
}
