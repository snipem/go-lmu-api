// Live standings monitor for LMU.
// Polls /rest/watch/standings and /rest/watch/standings/history every second.
//
// Usage: go run ./cmd/standings [-base http://localhost:6397] [-interval 1s]
package main

import (
	"bytes"
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
	TimeBehindNext   float64 `json:"timeBehindNext"`
	LapsBehindLeader int     `json:"lapsBehindLeader"`
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

type SessionInfo struct {
	Session string `json:"session"`
}

var maxSpeeds = map[int]float64{}

func main() {
	baseURL := flag.String("base", "http://localhost:6397", "Base URL of the API")
	interval := flag.Duration("interval", 1*time.Second, "Poll interval")
	flag.Parse()

	client := &http.Client{Timeout: 5 * time.Second}

	// Initial clear + hide cursor
	fmt.Print("\033[2J\033[?25l")
	defer fmt.Print("\033[?25h") // restore cursor on exit

	for {
		standings, err := fetchStandings(client, *baseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\rError: %v", err)
			time.Sleep(*interval)
			continue
		}
		history, _ := fetchHistory(client, *baseURL)
		var si SessionInfo
		fetchJSON(client, *baseURL+"/rest/watch/sessionInfo", &si)
		render(standings, history, si)
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

func lastLapFromHistory(laps []HistoryLap) (s1, s2, s3, lap float64) {
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

func isRaceSession(si SessionInfo) bool {
	s := strings.ToUpper(si.Session)
	return strings.Contains(s, "RACE")
}

func render(standings []Standing, history map[int][]HistoryLap, si SessionInfo) {
	sort.Slice(standings, func(i, j int) bool {
		return standings[i].Position < standings[j].Position
	})

	classCount := map[string]int{}
	pic := map[int]int{}
	for _, s := range standings {
		classCount[s.CarClass]++
		pic[s.SlotID] = classCount[s.CarClass]
	}

	for _, s := range standings {
		spd := s.CarVelocity.Velocity * 3.6
		if spd > maxSpeeds[s.SlotID] {
			maxSpeeds[s.SlotID] = spd
		}
	}

	race := isRaceSession(si)

	// Compute gaps: in race use timeBehindLeader, otherwise use best lap delta to P1
	var leaderBest float64
	if !race && len(standings) > 0 {
		leaderBest = standings[0].BestLapTime
	}

	// Build entire frame into a buffer, then write once
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "\033[H") // cursor home
	fmt.Fprintf(&buf, "  LMU Live  |  %s  |  %s  |  %d cars\033[K\n\n",
		strings.ToUpper(si.Session), time.Now().Format("15:04:05"), len(standings))

	hdr := fmt.Sprintf(
		"%3s %4s  %-16s %-22s %-5s %3s %4s %8s %7s %7s %7s %8s %8s %5s %3s",
		"P", "#", "Team", "Driver", "Cls", "PIC", "Laps", "Gap", "S1", "S2", "S3", "Last", "Best", "Vmax", "Pit",
	)
	fmt.Fprintf(&buf, "%s\033[K\n", hdr)
	fmt.Fprintf(&buf, "%s\033[K\n", strings.Repeat("─", len(hdr)))

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

		var s1, s2, s3 float64
		if laps, ok := history[s.SlotID]; ok && len(laps) > 0 {
			s1, s2, s3, _ = lastLapFromHistory(laps)
		}

		// Gap computation
		var gap string
		if s.Position == 1 {
			gap = "     ---"
		} else if race {
			// Race: use timeBehindLeader (laps behind shown as +NL)
			if s.LapsBehindLeader > 0 {
				gap = fmt.Sprintf("   +%dL", s.LapsBehindLeader)
			} else if s.TimeBehindLeader > 0 {
				gap = fmtGap(s.TimeBehindLeader)
			} else {
				gap = "     ---"
			}
		} else {
			// Practice/Quali: show best lap delta to P1's best
			if leaderBest > 0 && s.BestLapTime > 0 {
				delta := s.BestLapTime - leaderBest
				if delta > 0.001 {
					gap = fmtGap(delta)
				} else {
					gap = "     ---"
				}
			} else {
				gap = "   --.--"
			}
		}

		status := ""
		if s.PitState != "NONE" || s.InGarageStall {
			status = " PIT"
		}

		marker := " "
		if s.Player {
			marker = ">"
		}

		line := fmt.Sprintf(
			"%s%2d %4s  %-16s %-22s %-5s %3d %4d %8s %7s %7s %7s %8s %8s %5.0f %3d%s",
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

		if s.Player {
			// Bright cyan foreground + bold
			fmt.Fprintf(&buf, "\033[1;36m%s\033[0m\033[K\n", line)
		} else {
			fmt.Fprintf(&buf, "%s\033[K\n", line)
		}
	}
	fmt.Fprintf(&buf, "\033[J") // clear below

	os.Stdout.Write(buf.Bytes())
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

func fmtGap(t float64) string {
	if t < 60 {
		return fmt.Sprintf("+%6.2f", t)
	}
	mins := int(t) / 60
	secs := t - float64(mins*60)
	return fmt.Sprintf("+%d:%05.2f", mins, secs)
}
