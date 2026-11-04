package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	// Nhưng tzdata để LoadLocation("Asia/Ho_Chi_Minh") chạy được
	// cả trên Windows (noi không có file zoneinfo IANA).
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
		fmt.Printf("[watcher] không ghi được state: %v\n", err)
	}
}

// vnNow trả về ngay (YYYY-MM-DD) + số phút trong ngay theo giờ VN,
// giống Intl.DateTimeFormat timeZone Asia/Ho_Chi_Minh bản Bun.
func vnNow() (string, int) {
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		loc = time.FixedZone("ICT", 7*3600)
	}
	now := time.Now().In(loc)
	return now.Format("2006-01-02"), now.Hour()*60 + now.Minute()
}

// runReminder port popup-watcher.ts: không gọi API, không đọc token.
// Den khung giờ deal đêm (mặc định 17:55-22:10 giờ VN) thì báo
// 1 lần/ngày. --test báo ngay không đợi khung giờ.
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
			"Tới giờ nhận deal đêm ZCode!",
			"",
			"1. Mở ZCode.",
			"2. Bấm banner/modal deal đêm trong app.",
			"3. Từ 22:00 mới dùng được, tới 08:00 sáng hôm sau reset.",
			"4. Nhớ chọn đúng GLM-5.3-Flash + Global Building.",
			"",
			"Tool này chỉ báo, không tự bấm nhận thay bạn.",
		}, "\n")
		Notify("ZCode - Tới giờ nhận deal đêm!", message)
		fmt.Println(message)
		if autoOpen {
			fmt.Println(OpenZcode())
		}
	}

	if bools["test"] {
		showReminder()
		return 0
	}

	fmt.Printf("[watcher] Đang chạy nền. Sẽ báo trong khung %s-%s giờ VN, 1 lần/ngày.\n", fromStr, toStr)
	for {
		date, minutes := vnNow()
		if loadNotifiedDate() != date && minutes >= fromMinutes && minutes <= toMinutesValue {
			showReminder()
			saveNotifiedDate(date)
		}
		time.Sleep(60 * time.Second)
	}
}
