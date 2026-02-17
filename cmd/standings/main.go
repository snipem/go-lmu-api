// Live standings monitor for LMU.
// Polls /rest/watch/standings and /rest/watch/standings/history every second.
//
// Usage: go run ./cmd/standings [-base http://localhost:6397] [-interval 1s]
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-lmu-api/lib"
)

var maxSpeeds = map[int]float64{}

func main() {
	baseURL := flag.String("base", "http://localhost:6397", "Base URL of the API")
	interval := flag.Duration("interval", 1*time.Second, "Poll interval")
	flag.Parse()

	client := lib.NewClient(*baseURL)

	// Initial clear + hide cursor
	fmt.Print("\033[2J\033[?25l")
	defer fmt.Print("\033[?25h")

	for {
		standings, err := client.RestWatchStandings()
		if err != nil {
			fmt.Fprintf(os.Stderr, "\rError: %v", err)
			time.Sleep(*interval)
			continue
		}

		historyRaw, _ := client.RestWatchStandingsHistory()
		history := convertHistory(historyRaw)

		si, _ := client.RestWatchSessionInfo()
		var session string
		if si != nil {
			session = si.Session
		}

		render(standings, history, session)
		time.Sleep(*interval)
	}
}

func convertHistory(raw *map[string][]lib.RestWatchStandingsHistoryResponseItemItem) map[int][]lib.RestWatchStandingsHistoryResponseItemItem {
	if raw == nil {
		return nil
	}
	result := make(map[int][]lib.RestWatchStandingsHistoryResponseItemItem, len(*raw))
	for k, v := range *raw {
		id, _ := strconv.Atoi(k)
		result[id] = v
	}
	return result
}

func lastLapFromHistory(laps []lib.RestWatchStandingsHistoryResponseItemItem) (s1, s2, s3 float64) {
	for i := len(laps) - 1; i >= 0; i-- {
		l := laps[i]
		if l.LapTime > 0 && l.SectorTime1 > 0 && l.SectorTime2 > 0 {
			s1 = l.SectorTime1
			s2 = l.SectorTime2 - l.SectorTime1
			s3 = l.LapTime - l.SectorTime2
			return
		}
	}
	return
}

func isRaceSession(session string) bool {
	return strings.Contains(strings.ToUpper(session), "RACE")
}

func render(standings []lib.RestWatchStandingsResponseItem, history map[int][]lib.RestWatchStandingsHistoryResponseItemItem, session string) {
	sort.Slice(standings, func(i, j int) bool {
		return standings[i].Position < standings[j].Position
	})

	classCount := map[int]int{}
	pic := map[int]int{}
	for _, s := range standings {
		classCount[int(s.Position)]++
	}
	// Recompute PIC by iterating sorted standings grouped by class
	classPosCounter := map[string]int{}
	for _, s := range standings {
		classPosCounter[s.CarClass]++
		pic[int(s.SlotID)] = classPosCounter[s.CarClass]
	}

	for _, s := range standings {
		spd := s.CarVelocity.Velocity * 3.6
		slot := int(s.SlotID)
		if spd > maxSpeeds[slot] {
			maxSpeeds[slot] = spd
		}
	}

	race := isRaceSession(session)

	var leaderBest float64
	if !race && len(standings) > 0 {
		leaderBest = standings[0].BestLapTime
	}

	var buf bytes.Buffer

	fmt.Fprintf(&buf, "\033[H")
	sessionLabel := session
	if sessionLabel == "" {
		sessionLabel = "---"
	}
	fmt.Fprintf(&buf, "  LMU Live  |  %s  |  %s  |  %d cars\033[K\n\n",
		strings.ToUpper(sessionLabel), time.Now().Format("15:04:05"), len(standings))

	hdr := fmt.Sprintf(
		"%3s %4s  %-16s %-22s %-5s %3s %4s %8s %7s %7s %7s %8s %8s %5s %3s",
		"P", "#", "Team", "Driver", "Cls", "PIC", "Laps", "Gap", "S1", "S2", "S3", "Last", "Best", "Vmax", "Pit",
	)
	fmt.Fprintf(&buf, "%s\033[K\n", hdr)
	fmt.Fprintf(&buf, "%s\033[K\n", strings.Repeat("─", len(hdr)))

	for _, s := range standings {
		slot := int(s.SlotID)

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
		if laps, ok := history[slot]; ok && len(laps) > 0 {
			s1, s2, s3 = lastLapFromHistory(laps)
		}

		var gap string
		if s.Position == 1 {
			gap = "     ---"
		} else if race {
			if s.LapsBehindLeader > 0 {
				gap = fmt.Sprintf("   +%.0fL", s.LapsBehindLeader)
			} else if s.TimeBehindLeader > 0 {
				gap = fmtGap(s.TimeBehindLeader)
			} else {
				gap = "     ---"
			}
		} else {
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

		marker := " "
		if s.Player {
			marker = ">"
		}

		status := ""
		if s.PitState != "NONE" || s.InGarageStall {
			status = " PIT"
		}

		line := fmt.Sprintf(
			"%s%2.0f %4s  %-16s %-22s %-5s %3d %4.0f %8s %7s %7s %7s %8s %8s %5.0f %3.0f%s",
			marker,
			s.Position,
			carNum,
			team,
			driver,
			s.CarClass,
			pic[slot],
			s.LapsCompleted,
			gap,
			fmtSec(s1), fmtSec(s2), fmtSec(s3),
			fmtLap(s.LastLapTime),
			fmtLap(s.BestLapTime),
			maxSpeeds[slot],
			s.Pitstops,
			status,
		)

		if s.Player {
			fmt.Fprintf(&buf, "\033[1;36m%s\033[0m\033[K\n", line)
		} else {
			fmt.Fprintf(&buf, "%s\033[K\n", line)
		}
	}
	fmt.Fprintf(&buf, "\033[J")

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
