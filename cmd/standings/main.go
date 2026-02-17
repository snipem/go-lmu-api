// Live standings monitor for LMU.
// Polls /rest/watch/standings and /rest/watch/standings/history every second.
//
// Usage: go run ./cmd/standings [-base http://localhost:6397] [-interval 1s]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Standing struct {
	Position         int     `json:"position"`
	CarNumber        string  `json:"carNumber"`
	DriverName       string  `json:"driverName"`
	FullTeamName     string  `json:"fullTeamName"`
	VehicleName      string  `json:"vehicleName"`
	CarClass         string  `json:"carClass"`
	LapsCompleted    int     `json:"lapsCompleted"`
	LastLapTime      float64 `json:"lastLapTime"`
	BestLapTime      float64 `json:"bestLapTime"`
	TimeBehindLeader float64 `json:"timeBehindLeader"`
	Pitstops         int     `json:"pitstops"`
	PitState         string  `json:"pitState"`
	Player           bool    `json:"player"`
	InGarageStall    bool    `json:"inGarageStall"`
	SlotID           int     `json:"slotID"`
	CarVelocity      struct {
		Velocity float64 `json:"velocity"`
	} `json:"carVelocity"`
}

type HistoryLap struct {
	SlotID      int     `json:"slotID"`
	Position    int     `json:"position"`
	SectorTime1 float64 `json:"sectorTime1"`
	SectorTime2 float64 `json:"sectorTime2"`
	LapTime     float64 `json:"lapTime"`
	Pitting     bool    `json:"pitting"`
	DriverName  string  `json:"driverName"`
	CarClass    string  `json:"carClass"`
	VehicleName string  `json:"vehicleName"`
	TotalLaps   int     `json:"totalLaps"`
}

// Per-slot accumulated data across ticks.
var maxSpeeds = map[int]float64{}

func main() {
	baseURL := flag.String("base", "http://localhost:6397", "Base URL of the API")
	interval := flag.Duration("interval", 1*time.Second, "Poll interval")
	flag.Parse()

	client := &http.Client{Timeout: 5 * time.Second}

	for {
		standings, err := fetchStandings(client, *baseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\rError: %v", err)
			time.Sleep(*interval)
			continue
		}
		history, _ := fetchHistory(client, *baseURL)
		render(standings, history)
		time.Sleep(*interval)
	}
}

func fetchJSON(client *http.Client, url string, target interface{}) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(body, target)
}

func fetchStandings(client *http.Client, base string) ([]Standing, error) {
	var s []Standing
	err := fetchJSON(client, base+"/rest/watch/standings", &s)
	return s, err
}

func fetchHistory(client *http.Client, base string) (map[int][]HistoryLap, error) {
	var raw map[string][]HistoryLap
	if err := fetchJSON(client, base+"/rest/watch/standings/history", &raw); err != nil {
		return nil, err
	}
	result := make(map[int][]HistoryLap, len(raw))
	for k, v := range raw {
		id, _ := strconv.Atoi(k)
		result[id] = v
	}
	return result, nil
}

// lastLapFromHistory returns sector and lap times for the most recent completed lap.
func lastLapFromHistory(laps []HistoryLap) (s1, s2, s3, lap float64) {
	// Walk backwards to find last valid lap
	for i := len(laps) - 1; i >= 0; i-- {
		l := laps[i]
		if l.LapTime > 0 && l.SectorTime1 > 0 && l.SectorTime2 > 0 {
			s1 = l.SectorTime1
			s2 = l.SectorTime2 - l.SectorTime1
			s3 = l.LapTime - l.SectorTime2
			lap = l.LapTime
			return
		}
	}
	return
}

func render(standings []Standing, history map[int][]HistoryLap) {
	sort.Slice(standings, func(i, j int) bool {
		return standings[i].Position < standings[j].Position
	})

	// Position in class
	classCount := map[string]int{}
	pic := map[int]int{}
	for _, s := range standings {
		classCount[s.CarClass]++
		pic[s.SlotID] = classCount[s.CarClass]
	}

	// Track max speeds
	for _, s := range standings {
		spd := s.CarVelocity.Velocity * 3.6
		if spd > maxSpeeds[s.SlotID] {
			maxSpeeds[s.SlotID] = spd
		}
	}

	// Clear screen
	fmt.Print("\033[2J\033[H")
	fmt.Printf("  LMU Live Standings  |  %s  |  %d cars\n\n",
		time.Now().Format("15:04:05"), len(standings))

	hdr := fmt.Sprintf(
		"%3s %4s  %-16s %-22s %-5s %3s %4s %8s %7s %7s %7s %8s %8s %5s %3s",
		"P", "#", "Team", "Driver", "Cls", "PIC", "Laps", "Gap", "S1", "S2", "S3", "Last", "Best", "Vmax", "Pit",
	)
	fmt.Println(hdr)
	fmt.Println(strings.Repeat("─", len(hdr)))

	for _, s := range standings {
		carNum := s.CarNumber
		if carNum == "" {
			carNum = extractCarNum(s.VehicleName)
		}

		team := truncate(s.FullTeamName, 16)
		if team == "" {
			team = truncate(extractTeam(s.VehicleName), 16)
		}

		driver := truncate(s.DriverName, 22)

		// Sector times from history (more reliable, includes S3)
		var s1, s2, s3 float64
		if laps, ok := history[s.SlotID]; ok && len(laps) > 0 {
			s1, s2, s3, _ = lastLapFromHistory(laps)
		}

		gap := formatGap(s.TimeBehindLeader)

		// Highlight player
		marker := " "
		if s.Player {
			marker = ">"
		}

		// Pit indicator suffix
		status := ""
		if s.PitState != "NONE" || s.InGarageStall {
			status = " PIT"
		}

		fmt.Printf(
			"%s%2d %4s  %-16s %-22s %-5s %3d %4d %8s %7s %7s %7s %8s %8s %5.0f %3d%s\n",
			marker,
			s.Position,
			carNum,
			team,
			driver,
			s.CarClass,
			pic[s.SlotID],
			s.LapsCompleted,
			gap,
			fmtSec(s1), fmtSec(s2), fmtSec(s3),
			fmtLap(s.LastLapTime),
			fmtLap(s.BestLapTime),
			maxSpeeds[s.SlotID],
			s.Pitstops,
			status,
		)
	}
}

func extractCarNum(vn string) string {
	idx := strings.LastIndex(vn, "#")
	if idx < 0 {
		return "-"
	}
	rest := vn[idx+1:]
	if ci := strings.IndexByte(rest, ':'); ci >= 0 {
		return rest[:ci]
	}
	return rest
}

func extractTeam(vn string) string {
	idx := strings.LastIndex(vn, "#")
	if idx <= 0 {
		return vn
	}
	name := strings.TrimSpace(vn[:idx])
	if len(name) >= 5 && name[len(name)-4] >= '1' && name[len(name)-4] <= '2' {
		name = strings.TrimSpace(name[:len(name)-4])
	}
	return name
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func fmtLap(t float64) string {
	if t <= 0 {
		return "    -.--"
	}
	mins := int(t) / 60
	secs := t - float64(mins*60)
	if mins > 0 {
		return fmt.Sprintf("%d:%06.3f", mins, secs)
	}
	return fmt.Sprintf("%7.3f", secs)
}

func fmtSec(t float64) string {
	if t <= 0 {
		return "   -.--"
	}
	return fmt.Sprintf("%7.2f", t)
}

func formatGap(t float64) string {
	if t <= 0.0001 {
		return "  Leader"
	}
	if t < 60 {
		return fmt.Sprintf("+%6.2f", t)
	}
	mins := int(t) / 60
	secs := t - float64(mins*60)
	return fmt.Sprintf("+%d:%05.2f", mins, secs)
}
