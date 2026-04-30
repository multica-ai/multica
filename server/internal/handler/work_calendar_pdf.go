package handler

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"rsc.io/pdf"
)

// CalendarDayResponse is the JSON shape for a single day in a work calendar.
type CalendarDayResponse struct {
	Date  string  `json:"date"`
	Type  string  `json:"type"`
	Hours float64 `json:"hours"`
	Label string  `json:"label,omitempty"`
}

// MonthlyHoursResponse is the JSON shape for monthly hour totals.
type MonthlyHoursResponse struct {
	Month      int     `json:"month"`
	TotalHours float64 `json:"total_hours"`
}

type parsedCalendar struct {
	Year         int32
	Days         []CalendarDayResponse
	MonthlyHours []MonthlyHoursResponse
}

const (
	hoursNormal  = 8.5
	hoursReduced = 7.0
	hoursOff     = 0.0
)

// parseWorkCalendarPDF extracts calendar from PDF using cell background colors.
// Green = holiday (0h), Blue = reduced (7h), White = normal (8.5h), Weekends = 0h.
func parseWorkCalendarPDF(data []byte) (*parsedCalendar, error) {
	if len(data) < 5 || string(data[:5]) != "%PDF-" {
		return nil, fmt.Errorf("not a valid PDF file")
	}
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse PDF: %w", err)
	}

	// Extract year from positioned text (merging adjacent digit chars)
	year, err := extractYearFromPage(r)
	if err != nil {
		return nil, err
	}

	// Scan page: colored rects + text positions
	greenRects, blueRects, headerRects, dayNums := scanPage(r)

	// Determine grid layout from header rects (4 rows × 3 columns = 12 months)
	grid := buildGrid(headerRects)

	// Map each day number to a month, then check its color
	dayColors := make(map[[2]int]int) // [month, day] → 0=normal, 1=green, 2=blue
	for _, dn := range dayNums {
		month := grid.monthAt(dn.x, dn.y)
		if month < 1 || month > 12 {
			continue
		}
		if dn.num < 1 || dn.num > daysInMonthCount(year, month) {
			continue
		}
		if isInsideAny(greenRects, dn.x, dn.y) {
			dayColors[[2]int{month, dn.num}] = 1
		} else if isInsideAny(blueRects, dn.x, dn.y) {
			dayColors[[2]int{month, dn.num}] = 2
		}
	}

	// Build calendar
	days := buildCalendar(year, dayColors)
	return &parsedCalendar{
		Year:         year,
		Days:         days,
		MonthlyHours: computeMonthlyHours(days),
	}, nil
}

// --- Scanning ---

type rect struct{ x1, y1, x2, y2 float64 }
type dayNum struct {
	x, y float64
	num  int
}

func scanPage(r *pdf.Reader) (green, blue, headers []rect, days []dayNum) {
	for i := 1; i <= r.NumPage(); i++ {
		page := r.Page(i)
		g, b, h := extractRects(page)
		green = append(green, g...)
		blue = append(blue, b...)
		headers = append(headers, h...)
		days = append(days, extractDayNums(page)...)
	}
	return
}

type colorClass int

const (
	colorNone   colorClass = iota
	colorGreen             // holiday
	colorBlue              // reduced
	colorHeader            // peach/salmon header
	colorOther             // red weekends, dark title, etc.
)

func classifyRGB(r, g, b float64) colorClass {
	h, s, l := rgbToHSL(r, g, b)
	// Very dark or very light unsaturated → skip
	if s < 0.1 {
		return colorNone
	}
	// Green: H 80-160
	if h >= 80 && h <= 160 && s > 0.15 && l > 0.4 {
		return colorGreen
	}
	// Blue: H 180-260 (but not very dark)
	if h >= 180 && h <= 260 && s > 0.15 && l > 0.4 {
		return colorBlue
	}
	// Peach/orange header: H 10-40, high lightness
	if h >= 10 && h <= 40 && l > 0.8 {
		return colorHeader
	}
	return colorOther
}

func rgbToHSL(r, g, b float64) (float64, float64, float64) {
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	l := (max + min) / 2
	if max == min {
		return 0, 0, l
	}
	d := max - min
	var s float64
	if l > 0.5 {
		s = d / (2 - max - min)
	} else {
		s = d / (max + min)
	}
	var h float64
	switch max {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	case b:
		h = (r-g)/d + 4
	}
	h *= 60
	return h, s, l
}

// extractRects scans page content stream for colored filled rectangles.
func extractRects(page pdf.Page) (green, blue, headers []rect) {
	var fillR, fillG, fillB float64 = 1, 1, 1
	type pendRect struct{ x, y, w, h float64 }
	var pending *pendRect

	interpret := func(v pdf.Value) {
		pdf.Interpret(v, func(stk *pdf.Stack, op string) {
			n := stk.Len()
			args := make([]pdf.Value, n)
			for k := n - 1; k >= 0; k-- {
				args[k] = stk.Pop()
			}
			switch op {
			case "rg":
				if len(args) >= 3 {
					fillR, fillG, fillB = args[0].Float64(), args[1].Float64(), args[2].Float64()
				}
			case "g":
				if len(args) >= 1 {
					v := args[0].Float64()
					fillR, fillG, fillB = v, v, v
				}
			case "k":
				if len(args) >= 4 {
					c, m, y, k := args[0].Float64(), args[1].Float64(), args[2].Float64(), args[3].Float64()
					fillR, fillG, fillB = (1-c)*(1-k), (1-m)*(1-k), (1-y)*(1-k)
				}
			case "scn", "sc":
				switch len(args) {
				case 1:
					v := args[0].Float64()
					fillR, fillG, fillB = v, v, v
				case 3:
					fillR, fillG, fillB = args[0].Float64(), args[1].Float64(), args[2].Float64()
				case 4:
					c, m, y, k := args[0].Float64(), args[1].Float64(), args[2].Float64(), args[3].Float64()
					fillR, fillG, fillB = (1-c)*(1-k), (1-m)*(1-k), (1-y)*(1-k)
				}
			case "re":
				if len(args) >= 4 {
					pending = &pendRect{args[0].Float64(), args[1].Float64(), args[2].Float64(), args[3].Float64()}
				}
			case "f", "F", "f*", "B", "B*", "b", "b*":
				if pending != nil {
					cls := classifyRGB(fillR, fillG, fillB)
					r := rect{pending.x, pending.y, pending.x + pending.w, pending.y + pending.h}
					switch cls {
					case colorGreen:
						green = append(green, r)
					case colorBlue:
						blue = append(blue, r)
					case colorHeader:
						headers = append(headers, r)
					}
					pending = nil
				}
			case "n", "S":
				pending = nil
			case "m", "l", "c", "v", "y", "h":
				pending = nil
			}
		})
	}

	// Process main content stream
	interpret(page.V.Key("Contents"))

	// Process Form XObjects
	xobjects := page.V.Key("Resources").Key("XObject")
	for _, name := range xobjects.Keys() {
		xobj := xobjects.Key(name)
		if xobj.Key("Subtype").Name() == "Form" {
			interpret(xobj)
		}
	}

	return
}

// extractDayNums extracts day numbers by merging adjacent digit characters.
func extractDayNums(page pdf.Page) []dayNum {
	content := page.Content()
	if len(content.Text) == 0 {
		return nil
	}

	// Sort by Y descending then X ascending
	texts := make([]pdf.Text, len(content.Text))
	copy(texts, content.Text)
	sort.Slice(texts, func(a, b int) bool {
		if math.Abs(texts[a].Y-texts[b].Y) > 1 {
			return texts[a].Y > texts[b].Y
		}
		return texts[a].X < texts[b].X
	})

	// Merge adjacent digits on the same line into numbers
	var results []dayNum
	i := 0
	for i < len(texts) {
		t := texts[i]
		if len(t.S) == 1 && t.S[0] >= '0' && t.S[0] <= '9' {
			// Start collecting digits
			numStr := t.S
			startX := t.X
			j := i + 1
			for j < len(texts) && math.Abs(texts[j].Y-t.Y) < 1 {
				gap := texts[j].X - (texts[j-1].X + texts[j-1].W)
				if gap < 4 && len(texts[j].S) == 1 && texts[j].S[0] >= '0' && texts[j].S[0] <= '9' {
					numStr += texts[j].S
					j++
				} else {
					break
				}
			}
			num, _ := strconv.Atoi(numStr)
			if num >= 1 && num <= 31 {
				results = append(results, dayNum{x: startX, y: t.Y, num: num})
			}
			i = j
		} else {
			i++
		}
	}
	return results
}

// --- Grid layout ---

type monthGrid struct {
	cols [3]float64 // X start of each column
	rows [4]float64 // Y start of each row (top to bottom)
	colW float64    // column width
	rowH float64    // row height
}

// monthAt returns the month (1-12) for a given position, or 0 if outside grid.
func (g *monthGrid) monthAt(x, y float64) int {
	col := -1
	for c := 0; c < 3; c++ {
		if x >= g.cols[c]-5 && x <= g.cols[c]+g.colW+5 {
			col = c
			break
		}
	}
	if col < 0 {
		return 0
	}

	row := -1
	for r := 0; r < 4; r++ {
		// Day numbers are BELOW the header, within ~60 units
		if y <= g.rows[r]+5 && y >= g.rows[r]-65 {
			row = r
			break
		}
	}
	if row < 0 {
		return 0
	}

	return row*3 + col + 1
}

// buildGrid determines the 4×3 month grid from header rectangles.
func buildGrid(headers []rect) *monthGrid {
	if len(headers) < 3 {
		return defaultGrid()
	}

	// Sort by Y descending (top rows first in PDF coords), then by X
	sort.Slice(headers, func(i, j int) bool {
		if math.Abs(headers[i].y1-headers[j].y1) > 10 {
			return headers[i].y1 > headers[j].y1
		}
		return headers[i].x1 < headers[j].x1
	})

	// Group into rows (similar Y)
	var rows [][]rect
	var currentRow []rect
	for _, h := range headers {
		if len(currentRow) == 0 || math.Abs(h.y1-currentRow[0].y1) < 10 {
			currentRow = append(currentRow, h)
		} else {
			rows = append(rows, currentRow)
			currentRow = []rect{h}
		}
	}
	if len(currentRow) > 0 {
		rows = append(rows, currentRow)
	}

	g := &monthGrid{}
	// Get column positions from first row with 3 headers
	for _, row := range rows {
		if len(row) >= 3 {
			sort.Slice(row, func(i, j int) bool { return row[i].x1 < row[j].x1 })
			g.cols = [3]float64{row[0].x1, row[1].x1, row[2].x1}
			g.colW = row[0].x2 - row[0].x1
			break
		}
	}

	// Get row Y positions
	for i := 0; i < 4 && i < len(rows); i++ {
		g.rows[i] = rows[i][0].y1
	}

	// Estimate row height from spacing
	if len(rows) >= 2 {
		g.rowH = math.Abs(rows[0][0].y1 - rows[1][0].y1)
	} else {
		g.rowH = 100
	}

	return g
}

func defaultGrid() *monthGrid {
	return &monthGrid{
		cols: [3]float64{70, 246, 422},
		rows: [4]float64{523, 400, 280, 160},
		colW: 137,
		rowH: 120,
	}
}

// --- Helpers ---

func isInsideAny(rects []rect, x, y float64) bool {
	for _, r := range rects {
		x1, x2 := r.x1, r.x2
		if x1 > x2 {
			x1, x2 = x2, x1
		}
		y1, y2 := r.y1, r.y2
		if y1 > y2 {
			y1, y2 = y2, y1
		}
		if x >= x1-2 && x <= x2+2 && y >= y1 && y <= y2 {
			return true
		}
	}
	return false
}

func buildCalendar(year int32, dayColors map[[2]int]int) []CalendarDayResponse {
	var days []CalendarDayResponse
	for month := 1; month <= 12; month++ {
		for d := 1; d <= daysInMonthCount(year, month); d++ {
			t := time.Date(int(year), time.Month(month), d, 0, 0, 0, 0, time.UTC)
			dateStr := t.Format("2006-01-02")
			if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
				days = append(days, CalendarDayResponse{Date: dateStr, Type: "weekend", Hours: hoursOff})
				continue
			}
			switch dayColors[[2]int{month, d}] {
			case 1:
				days = append(days, CalendarDayResponse{Date: dateStr, Type: "holiday", Hours: hoursOff})
			case 2:
				days = append(days, CalendarDayResponse{Date: dateStr, Type: "reduced", Hours: hoursReduced})
			default:
				days = append(days, CalendarDayResponse{Date: dateStr, Type: "normal", Hours: hoursNormal})
			}
		}
	}
	return days
}

func computeMonthlyHours(days []CalendarDayResponse) []MonthlyHoursResponse {
	totals := make(map[int]float64)
	for _, d := range days {
		t, _ := time.Parse("2006-01-02", d.Date)
		totals[int(t.Month())] += d.Hours
	}
	var results []MonthlyHoursResponse
	for m := 1; m <= 12; m++ {
		results = append(results, MonthlyHoursResponse{Month: m, TotalHours: totals[m]})
	}
	return results
}

func daysInMonthCount(year int32, month int) int {
	return time.Date(int(year), time.Month(month+1), 0, 0, 0, 0, 0, time.UTC).Day()
}

// extractYearFromPage finds a 4-digit year by merging adjacent digit characters.
func extractYearFromPage(r *pdf.Reader) (int32, error) {
	for i := 1; i <= r.NumPage(); i++ {
		texts := r.Page(i).Content().Text
		for j := range texts {
			// Look for sequences of 4 adjacent digits forming 20xx
			if texts[j].S != "2" {
				continue
			}
			num := mergeAdjacentDigits(texts, j)
			if len(num) == 4 && num[:2] == "20" {
				y, _ := strconv.Atoi(num)
				if y >= 2020 && y <= 2040 {
					return int32(y), nil
				}
			}
		}
	}
	return 0, fmt.Errorf("no year found in PDF")
}

// mergeAdjacentDigits merges consecutive digit text elements starting at index.
func mergeAdjacentDigits(texts []pdf.Text, start int) string {
	result := texts[start].S
	for i := start + 1; i < len(texts) && i < start+4; i++ {
		// Must be on same line (Y within 1) and close in X (gap < 5)
		if math.Abs(texts[i].Y-texts[start].Y) > 1 {
			break
		}
		gap := texts[i].X - (texts[i-1].X + texts[i-1].W)
		if gap > 5 {
			break
		}
		if len(texts[i].S) != 1 || texts[i].S[0] < '0' || texts[i].S[0] > '9' {
			break
		}
		result += texts[i].S
	}
	return result
}
