package qrcode

import (
	"bytes"
	"errors"
	"fmt"
)

// Version specs for EC Level L (1..10): {totalCodewords, ecCodewordsPerBlock, numBlocks}
type verSpec struct {
	ver        int
	size       int
	dataBytes  int
	ecPerBlock int
	blocks     int
	align      []int
}

var versionsL = []verSpec{
	{1, 21, 19, 7, 1, nil},
	{2, 25, 34, 10, 1, []int{6, 18}},
	{3, 29, 55, 15, 1, []int{6, 22}},
	{4, 33, 80, 20, 1, []int{6, 26}},
	{5, 37, 108, 26, 1, []int{6, 30}},
	{6, 41, 136, 18, 2, []int{6, 34}},
	{7, 45, 156, 20, 2, []int{6, 22, 38}},
	{8, 49, 194, 24, 2, []int{6, 24, 42}},
	{9, 53, 232, 30, 2, []int{6, 26, 46}},
	{10, 57, 274, 18, 4, []int{6, 28, 50}}, // 2 blocks of 68 + 2 blocks of 69 = 274
}

// RenderSVG encodes UTF-8 text (up to ~270 bytes) into a clean, scan-ready QR Code SVG.
func RenderSVG(text string) ([]byte, error) {
	payload := []byte(text)
	if len(payload) == 0 {
		return nil, errors.New("empty qr payload")
	}
	var spec *verSpec
	for i := range versionsL {
		capBytes := versionsL[i].dataBytes - 2
		if versionsL[i].ver >= 10 {
			capBytes = versionsL[i].dataBytes - 3
		}
		if len(payload) <= capBytes {
			spec = &versionsL[i]
			break
		}
	}
	if spec == nil {
		return nil, errors.New("qr text exceeds version 10 capacity")
	}

	dataCW := encodeByteMode(payload, *spec)
	finalCW := interleaveWithEC(dataCW, *spec)
	mat := buildMatrix(*spec, finalCW)

	margin := 3
	total := spec.size + margin*2
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, total, total)
	fmt.Fprintf(&buf, `<rect width="%d" height="%d" rx="2.5" fill="#ffffff"/>`, total, total)
	buf.WriteString(`<path fill="#0f172a" d="`)
	for y := 0; y < spec.size; y++ {
		for x := 0; x < spec.size; x++ {
			if mat[y][x] {
				fmt.Fprintf(&buf, "M%d,%dh1v1h-1z", x+margin, y+margin)
			}
		}
	}
	buf.WriteString(`"/></svg>`)
	return buf.Bytes(), nil
}

func encodeByteMode(payload []byte, spec verSpec) []byte {
	bits := make([]uint8, 0, spec.dataBytes*8)
	appendBits := func(val int, n int) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, uint8((val>>i)&1))
		}
	}
	// Mode 0100 (Byte)
	appendBits(0b0100, 4)
	ccBits := 8
	if spec.ver >= 10 {
		ccBits = 16
	}
	appendBits(len(payload), ccBits)
	for _, b := range payload {
		appendBits(int(b), 8)
	}
	maxBits := spec.dataBytes * 8
	for i := 0; i < 4 && len(bits) < maxBits; i++ {
		bits = append(bits, 0)
	}
	for len(bits)%8 != 0 {
		bits = append(bits, 0)
	}
	out := make([]byte, 0, spec.dataBytes)
	for i := 0; i < len(bits); i += 8 {
		var b byte
		for j := 0; j < 8; j++ {
			b = (b << 1) | bits[i+j]
		}
		out = append(out, b)
	}
	pads := []byte{0xEC, 0x11}
	for p := 0; len(out) < spec.dataBytes; p++ {
		out = append(out, pads[p%2])
	}
	return out
}

// GF(256) with primitive polynomial 0x11D
var gfExp [512]byte
var gfLog [256]byte

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[x] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

func rsGeneratorPoly(degree int) []byte {
	poly := []byte{1}
	for i := 0; i < degree; i++ {
		next := make([]byte, len(poly)+1)
		root := gfExp[i]
		for j, c := range poly {
			next[j] ^= c
			next[j+1] ^= gfMul(c, root)
		}
		poly = next
	}
	return poly
}

func rsRemainder(data []byte, degree int) []byte {
	gen := rsGeneratorPoly(degree)
	rem := make([]byte, degree)
	for _, b := range data {
		factor := b ^ rem[0]
		copy(rem, rem[1:])
		rem[degree-1] = 0
		for i := 0; i < degree; i++ {
			rem[i] ^= gfMul(gen[i+1], factor)
		}
	}
	return rem
}

func interleaveWithEC(data []byte, spec verSpec) []byte {
	shortBlocks := spec.blocks - (spec.dataBytes % spec.blocks)
	shortLen := spec.dataBytes / spec.blocks

	dataBlocks := make([][]byte, spec.blocks)
	ecBlocks := make([][]byte, spec.blocks)
	offset := 0
	maxDataLen := 0
	for b := 0; b < spec.blocks; b++ {
		blen := shortLen
		if b >= shortBlocks {
			blen++
		}
		if blen > maxDataLen {
			maxDataLen = blen
		}
		blk := data[offset : offset+blen]
		offset += blen
		dataBlocks[b] = blk
		ecBlocks[b] = rsRemainder(blk, spec.ecPerBlock)
	}

	out := make([]byte, 0, spec.dataBytes+spec.blocks*spec.ecPerBlock)
	for i := 0; i < maxDataLen; i++ {
		for b := 0; b < spec.blocks; b++ {
			if i < len(dataBlocks[b]) {
				out = append(out, dataBlocks[b][i])
			}
		}
	}
	for i := 0; i < spec.ecPerBlock; i++ {
		for b := 0; b < spec.blocks; b++ {
			out = append(out, ecBlocks[b][i])
		}
	}
	return out
}

func buildMatrix(spec verSpec, codewords []byte) [][]bool {
	n := spec.size
	modules := make([][]bool, n)
	isFunc := make([][]bool, n)
	for i := range modules {
		modules[i] = make([]bool, n)
		isFunc[i] = make([]bool, n)
	}

	setFunc := func(x, y int, dark bool) {
		if x >= 0 && x < n && y >= 0 && y < n {
			modules[y][x] = dark
			isFunc[y][x] = true
		}
	}

	drawFinder := func(cx, cy int) {
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				xx, yy := cx+dx, cy+dy
				if xx < 0 || xx >= n || yy < 0 || yy >= n {
					continue
				}
				dist := maxInt(absInt(dx), absInt(dy))
				setFunc(xx, yy, dist != 2 && dist != 4)
			}
		}
	}

	drawFinder(3, 3)
	drawFinder(n-4, 3)
	drawFinder(3, n-4)

	for i := 0; i < n; i++ {
		if !isFunc[6][i] {
			setFunc(i, 6, i%2 == 0)
		}
		if !isFunc[i][6] {
			setFunc(6, i, i%2 == 0)
		}
	}

	for _, ay := range spec.align {
		for _, ax := range spec.align {
			if (ax == 6 && ay == 6) || (ax == 6 && ay == n-7) || (ax == n-7 && ay == 6) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					dist := maxInt(absInt(dx), absInt(dy))
					setFunc(ax+dx, ay+dy, dist != 1)
				}
			}
		}
	}

	// Reserve format info & dark module
	for i := 0; i <= 8; i++ {
		if i != 6 {
			setFunc(8, i, false)
			setFunc(i, 8, false)
		}
	}
	for i := 0; i < 8; i++ {
		setFunc(n-1-i, 8, false)
		if i < 7 {
			setFunc(8, n-1-i, false)
		}
	}
	setFunc(8, n-8, true) // Dark module

	if spec.ver >= 7 {
		// Version info bits (for ver 7..10)
		rem := spec.ver
		for i := 0; i < 12; i++ {
			rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
		}
		vbits := (spec.ver << 12) | rem
		for i := 0; i < 18; i++ {
			bit := ((vbits >> i) & 1) != 0
			a, b := n-11+i%3, i/3
			setFunc(a, b, bit)
			setFunc(b, a, bit)
		}
	}

	// Zigzag data placement
	bitIdx := 0
	totalBits := len(codewords) * 8
	for right := n - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < n; vert++ {
			for j := 0; j < 2; j++ {
				x := right - j
				upward := ((right + 1) & 2) == 0
				y := vert
				if upward {
					y = n - 1 - vert
				}
				if !isFunc[y][x] {
					dark := false
					if bitIdx < totalBits {
						dark = ((codewords[bitIdx>>3] >> (7 - (bitIdx & 7))) & 1) != 0
						bitIdx++
					}
					modules[y][x] = dark
				}
			}
		}
	}

	// Choose best mask (0..7)
	bestMask := 0
	bestPenalty := 1 << 30
	var bestGrid [][]bool
	for mask := 0; mask < 8; mask++ {
		cand := cloneGrid(modules)
		applyMask(cand, isFunc, mask)
		drawFormatBits(cand, n, mask)
		p := penaltyScore(cand)
		if p < bestPenalty {
			bestPenalty = p
			bestMask = mask
			bestGrid = cand
		}
	}
	_ = bestMask
	return bestGrid
}

func applyMask(grid [][]bool, isFunc [][]bool, mask int) {
	n := len(grid)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if isFunc[y][x] {
				continue
			}
			var invert bool
			switch mask {
			case 0:
				invert = (x+y)%2 == 0
			case 1:
				invert = y%2 == 0
			case 2:
				invert = x%3 == 0
			case 3:
				invert = (x+y)%3 == 0
			case 4:
				invert = (y/2+x/3)%2 == 0
			case 5:
				invert = (x*y)%2+(x*y)%3 == 0
			case 6:
				invert = ((x*y)%2+(x*y)%3)%2 == 0
			case 7:
				invert = ((x+y)%2+(x*y)%3)%2 == 0
			}
			if invert {
				grid[y][x] = !grid[y][x]
			}
		}
	}
}

func drawFormatBits(grid [][]bool, n int, mask int) {
	// EC Level L = 01
	data := (0b01 << 3) | mask
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	bits := ((data << 10) | rem) ^ 0x5412
	bit := func(i int) bool { return ((bits >> i) & 1) != 0 }

	for i := 0; i <= 5; i++ {
		grid[i][8] = bit(i)
	}
	grid[7][8] = bit(6)
	grid[8][8] = bit(7)
	grid[8][7] = bit(8)
	for i := 9; i < 15; i++ {
		grid[8][14-i] = bit(i)
	}
	for i := 0; i < 8; i++ {
		grid[8][n-1-i] = bit(i)
	}
	for i := 8; i < 15; i++ {
		grid[n-15+i][8] = bit(i)
	}
}

func penaltyScore(grid [][]bool) int {
	n := len(grid)
	score := 0
	for y := 0; y < n; y++ {
		runColor := grid[y][0]
		runLen := 1
		for x := 1; x < n; x++ {
			if grid[y][x] == runColor {
				runLen++
				if runLen == 5 {
					score += 3
				} else if runLen > 5 {
					score++
				}
			} else {
				runColor = grid[y][x]
				runLen = 1
			}
		}
	}
	for x := 0; x < n; x++ {
		runColor := grid[0][x]
		runLen := 1
		for y := 1; y < n; y++ {
			if grid[y][x] == runColor {
				runLen++
				if runLen == 5 {
					score += 3
				} else if runLen > 5 {
					score++
				}
			} else {
				runColor = grid[y][x]
				runLen = 1
			}
		}
	}
	for y := 0; y < n-1; y++ {
		for x := 0; x < n-1; x++ {
			c := grid[y][x]
			if c == grid[y][x+1] && c == grid[y+1][x] && c == grid[y+1][x+1] {
				score += 3
			}
		}
	}
	return score
}

func cloneGrid(src [][]bool) [][]bool {
	out := make([][]bool, len(src))
	for i := range src {
		out[i] = append([]bool(nil), src[i]...)
	}
	return out
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
