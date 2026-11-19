package server

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"sync"
)

var (
	touchIconOnce     sync.Once
	icon180PNG        []byte
	icon192PNG        []byte
	icon512PNG        []byte
	icon512MaskablePNG []byte
)

func ensureTouchIcons() {
	touchIconOnce.Do(func() {
		icon180PNG = renderStudioTouchIcon(180, false)
		icon192PNG = renderStudioTouchIcon(192, false)
		icon512PNG = renderStudioTouchIcon(512, false)
		icon512MaskablePNG = renderStudioTouchIcon(512, true)
	})
}

// handleWebManifest serves a PWA manifest under /assets/manifest.webmanifest
// so existing Cloudflare Access Bypass (/assets/*) lets iOS/Android fetch it without auth redirects.
func (s *Server) handleWebManifest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(`{
  "id": "/",
  "name": "O.Arc DROP",
  "short_name": "O.Arc Drop",
  "description": "跨端私密互传 · 三合一文件柜 · 黑胶歌词唱机",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "display_override": ["standalone", "minimal-ui"],
  "orientation": "any",
  "background_color": "#e8eaef",
  "theme_color": "#e8eaef",
  "icons": [
    {
      "src": "/assets/apple-touch-icon.png?v=150",
      "sizes": "180x180",
      "type": "image/png",
      "purpose": "any"
    },
    {
      "src": "/assets/icon-192.png?v=150",
      "sizes": "192x192",
      "type": "image/png",
      "purpose": "any"
    },
    {
      "src": "/assets/icon-512.png?v=150",
      "sizes": "512x512",
      "type": "image/png",
      "purpose": "any"
    },
    {
      "src": "/assets/icon-512-maskable.png?v=150",
      "sizes": "512x512",
      "type": "image/png",
      "purpose": "maskable"
    }
  ]
}`))
}

// handleAppleTouchIcon serves the exact 180x180 @3x iPhone Home Screen icon (opaque edge-to-edge,
// respecting Apple's continuous squircle mask without black corner artifacts).
func (s *Server) handleAppleTouchIcon(w http.ResponseWriter, r *http.Request) {
	ensureTouchIcons()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	_, _ = w.Write(icon180PNG)
}

// handleIcon192 serves the standard 192x192 Android launcher & PWA install icon.
func (s *Server) handleIcon192(w http.ResponseWriter, r *http.Request) {
	ensureTouchIcons()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	_, _ = w.Write(icon192PNG)
}

// handleIcon512 serves the 512x512 high-resolution studio icon for PWA splash & desktop installs.
func (s *Server) handleIcon512(w http.ResponseWriter, r *http.Request) {
	ensureTouchIcons()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	_, _ = w.Write(icon512PNG)
}

// handleIcon512Maskable serves a 512x512 Android adaptive maskable icon with W3C 80% safe-zone inset
// so circular / squircle / teardrop Android launcher masks never clip the studio dial or LED.
func (s *Server) handleIcon512Maskable(w http.ResponseWriter, r *http.Request) {
	ensureTouchIcons()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=604800, immutable")
	_, _ = w.Write(icon512MaskablePNG)
}

// renderStudioTouchIcon synthesizes an anti-aliased Precision Studio Hardware icon in pure Go:
// - Full-bleed top-lit anodized obsidian titanium surface (100% opaque for iOS/Android masks)
// - Machined recessed studio dial with top specular catch-light & micro-vinyl grooves
// - Signature O.Arc segmented transfer ring + optically balanced rounded-cap Drop arrow
// - Teenage-Engineering #EA580C status LED with warm glow & crisp core highlight
func renderStudioTouchIcon(size int, maskable bool) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	fSize := float64(size)
	center := fSize / 2.0
	inset := 1.0
	if maskable {
		inset = 0.78
	}
	scale := (fSize / 32.0) * inset

	dialR := 11.4 * scale
	outerRingR := 9.5 * scale
	outerRingW := 0.42 * scale
	innerRingR := 6.7 * scale
	innerRingW := 0.72 * scale

	// Position orange LED cleanly inside the top-right gap of the inner transfer ring
	ledAngle := -math.Pi / 4.0 // -45 deg (top-right)
	ledCx := center + math.Cos(ledAngle)*outerRingR
	ledCy := center + math.Sin(ledAngle)*outerRingR
	ledR := 1.48 * scale

	for y := 0; y < size; y++ {
		fy := float64(y) + 0.5
		ny := fy / fSize
		for x := 0; x < size; x++ {
			fx := float64(x) + 0.5
			dx := fx - center
			dy := fy - center
			dist := math.Hypot(dx, dy)

			// 1. Top-lit anodized obsidian background (#1B1F29 top -> #090B0F bottom)
			topGlow := math.Max(0, 1.0-math.Hypot(fx-center, fy-fSize*0.14)/(fSize*0.85))
			r := 9.0 + (1.0-ny)*11.0 + topGlow*13.0
			g := 11.0 + (1.0-ny)*12.0 + topGlow*15.0
			b := 15.0 + (1.0-ny)*16.0 + topGlow*20.0

			// 2. Central machined hardware dial well + top specular bevel
			if dist < dialR+scale*0.6 {
				wellAlpha := clamp01((dialR - dist) / (scale * 0.45))
				// Slightly deeper matte graphite inside dial + subtle radial sheen
				dialSheen := math.Max(0, 1.0-dist/dialR) * 9.0
				dr := 14.0 + dialSheen
				dg := 16.0 + dialSheen*1.06
				db := 22.0 + dialSheen*1.18

				// Subtle concentric vinyl micro-grooves inside dial
				if dist > innerRingR*1.08 && dist < outerRingR*0.94 {
					groove := 0.5 + 0.5*math.Sin(dist/(scale*0.38)*math.Pi*2)
					dr += groove * 2.6
					dg += groove * 2.8
					db += groove * 3.4
				}

				r = mix(r, dr, wellAlpha)
				g = mix(g, dg, wellAlpha)
				b = mix(b, db, wellAlpha)

				// Crisp 1px top-lit metallic rim around the dial
				dDialRim := math.Abs(dist - dialR)
				if dDialRim < 0.42*scale {
					rimLight := clamp01((-dy/dialR)*0.65 + 0.35)
					rimAlpha := clamp01((0.42*scale-dDialRim)/(scale*0.28)) * (0.14 + 0.28*rimLight)
					r = mix(r, 245, rimAlpha)
					g = mix(g, 247, rimAlpha)
					b = mix(b, 252, rimAlpha)
				}
			}

			// 3. Outer subtle precision track ring (leaves breathing room around orange LED)
			dLed := math.Hypot(fx-ledCx, fy-ledCy)
			dOuter := math.Abs(dist - outerRingR)
			if dOuter < outerRingW && dLed > ledR*1.55 {
				alpha := clamp01((outerRingW-dOuter)/(scale*0.26)) * 0.30
				r = mix(r, 240, alpha)
				g = mix(g, 243, alpha)
				b = mix(b, 249, alpha)
			}

			// 4. Inner high-contrast segmented transfer arc (C-arc with sleek gap at top-right)
			dInner := math.Abs(dist - innerRingR)
			if dInner < innerRingW {
				ang := math.Atan2(dy, dx) // [-pi, pi]
				// Gap centered at -pi/4 (top-right, ~ -0.785 rad), width ~ 0.95 rad
				angDiff := math.Abs(wrapAngle(ang - ledAngle))
				if angDiff > 0.46 {
					capFade := clamp01((angDiff - 0.46) / 0.08)
					alpha := clamp01((innerRingW-dInner)/(scale*0.28)) * 0.90 * capFade
					r = mix(r, 244, alpha)
					g = mix(g, 246, alpha)
					b = mix(b, 250, alpha)
				}
			}

			// 5. Optically centered crisp Drop arrow (stem + chevron)
			stemTopY := center - 3.6*scale
			stemBotY := center + 3.5*scale
			wingDx := 2.85 * scale
			wingTopY := center + 0.65*scale

			dStem := segDist(fx, fy, center, stemTopY, center, stemBotY)
			dTipL := segDist(fx, fy, center-wingDx, wingTopY, center, stemBotY)
			dTipR := segDist(fx, fy, center+wingDx, wingTopY, center, stemBotY)
			dArrow := math.Min(dStem, math.Min(dTipL, dTipR))
			arrowHalfW := 0.92 * scale
			if dArrow < arrowHalfW {
				alpha := clamp01((arrowHalfW - dArrow) / (scale * 0.28))
				r = mix(r, 255, alpha)
				g = mix(g, 255, alpha)
				b = mix(b, 255, alpha)
			}

			// 6. Signature #EA580C studio status LED + warm halo + specular pinpoint
			if dLed < ledR*2.8 {
				glow := math.Pow(clamp01(1.0-dLed/(ledR*2.8)), 2.3) * 0.44
				r = mix(r, 234, glow)
				g = mix(g, 88, glow)
				b = mix(b, 12, glow)
			}
			if dLed < ledR {
				alpha := clamp01((ledR - dLed) / (scale * 0.26))
				r = mix(r, 242, alpha)
				g = mix(g, 96, alpha)
				b = mix(b, 18, alpha)
				// Tiny hot specular highlight on upper-left of LED lens
				dSpec := math.Hypot(fx-(ledCx-0.38*scale), fy-(ledCy-0.38*scale))
				if dSpec < 0.55*scale {
					specA := clamp01(1.0-dSpec/(0.55*scale)) * 0.55
					r = mix(r, 255, specA)
					g = mix(g, 215, specA)
					b = mix(b, 180, specA)
				}
			}

			img.SetRGBA(x, y, color.RGBA{
				R: uint8(math.Min(255, math.Max(0, r))),
				G: uint8(math.Min(255, math.Max(0, g))),
				B: uint8(math.Min(255, math.Max(0, b))),
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func wrapAngle(a float64) float64 {
	for a > math.Pi {
		a -= 2 * math.Pi
	}
	for a < -math.Pi {
		a += 2 * math.Pi
	}
	return a
}

func segDist(px, py, ax, ay, bx, by float64) float64 {
	vx, vy := bx-ax, by-ay
	wx, wy := px-ax, py-ay
	c1 := vx*wx + vy*wy
	if c1 <= 0 {
		return math.Hypot(px-ax, py-ay)
	}
	c2 := vx*vx + vy*vy
	if c2 <= c1 {
		return math.Hypot(px-bx, py-by)
	}
	t := c1 / c2
	projX, projY := ax+t*vx, ay+t*vy
	return math.Hypot(px-projX, py-projY)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func mix(a, b, t float64) float64 {
	return a + (b-a)*t
}
