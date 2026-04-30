package handler

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"sort"
	"testing"

	"rsc.io/pdf"
)

func TestDiagRectVsText(t *testing.T) {
	data, err := os.ReadFile("testdata/calendario.pdf")
	if err != nil {
		t.Skip("no test PDF")
	}
	r, _ := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	page := r.Page(1)

	greenRects, _, _, _ := scanPage(r)

	// Get day numbers
	days := extractDayNums(page)

	// For Jan area - find day 22 and day 23 (or nearby holidays)
	// Print green rects in the January region and nearby day positions
	_, _, headers := extractRects(page)
	grid := buildGrid(headers)

	// Print all days that are inside a green rect with their month assignment
	fmt.Println("=== Days inside GREEN rects ===")
	for _, dn := range days {
		month := grid.monthAt(dn.x, dn.y)
		if isInsideAny(greenRects, dn.x, dn.y) {
			fmt.Printf("  Month %2d, Day %2d at (%.1f, %.1f)\n", month, dn.num, dn.x, dn.y)
		}
	}

	// For each green rect, find what day numbers are near it
	fmt.Println("\n=== Green rects with nearby day numbers ===")
	// Sort green rects by month
	sort.Slice(greenRects, func(i, j int) bool {
		mi := grid.monthAt((greenRects[i].x1+greenRects[i].x2)/2, (greenRects[i].y1+greenRects[i].y2)/2)
		mj := grid.monthAt((greenRects[j].x1+greenRects[j].x2)/2, (greenRects[j].y1+greenRects[j].y2)/2)
		return mi < mj
	})
	for _, gr := range greenRects {
		y1, y2 := gr.y1, gr.y2
		if y1 > y2 { y1, y2 = y2, y1 }
		x1, x2 := gr.x1, gr.x2
		if x1 > x2 { x1, x2 = x2, x1 }
		cx := (x1+x2)/2
		cy := (y1+y2)/2
		month := grid.monthAt(cx, cy)
		
		// Find day nums whose X overlaps and are close in Y
		var nearby []string
		for _, dn := range days {
			if dn.x >= x1-5 && dn.x <= x2+5 && math.Abs(dn.y - cy) < 20 {
				dm := grid.monthAt(dn.x, dn.y)
				nearby = append(nearby, fmt.Sprintf("d%d(m%d,y=%.1f)", dn.num, dm, dn.y))
			}
		}
		fmt.Printf("  GreenRect month=%d y=[%.1f..%.1f] cy=%.1f x=[%.1f..%.1f] nearby=%v\n", 
			month, y1, y2, cy, x1, x2, nearby)
	}
}
