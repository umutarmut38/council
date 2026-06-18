//go:build ignore

// meshgen bakes the EVA-01 head glTF into a compact embedded point cloud
// (evahead_data.go). It is stdlib-only and excluded from normal builds.
//
// Run from this directory:
//
//	go run meshgen.go
//
// or via `go generate ./internal/tui/anim/` (see the directive in head.go).
//
// Source asset (NOT committed — see .gitignore): testdata/scene.gltf +
// testdata/scene.bin, extracted from evangelion_unit_01_head_printable.zip.
//
// Model: "Evangelion Unit 01 head Printable" by ngkirto
// (https://sketchfab.com/3d-models/evangelion-unit-01-head-printable-3a8d3a39f72740fcb08760a12f97b0a3),
// licensed CC-BY-4.0 (https://creativecommons.org/licenses/by/4.0/).
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// targetVoxel is the downsample cell size in normalized units (the head is
// scaled to max-radius 1). Tuned so the cloud lands around 4-5k points while
// staying gap-free at the docked and intro sizes. Downsampling is per-region so
// small regions (eyes, red) are never absorbed by the armor.
const targetVoxel = 0.030

type gltf struct {
	Accessors []struct {
		BufferView int    `json:"bufferView"`
		ByteOffset int    `json:"byteOffset"`
		Count      int    `json:"count"`
		Type       string `json:"type"`
	} `json:"accessors"`
	BufferViews []struct {
		ByteOffset int `json:"byteOffset"`
		ByteStride int `json:"byteStride"`
	} `json:"bufferViews"`
	Meshes []struct {
		Name       string `json:"name"`
		Primitives []struct {
			Attributes map[string]int `json:"attributes"`
		} `json:"primitives"`
	} `json:"meshes"`
}

type pt struct {
	x, y, z    float64
	nx, ny, nz float64
}

func main() {
	// The raw mesh is gitignored (CC-BY, not redistributed), so it is usually
	// absent. Skip cleanly in that case — evahead_data.go is committed and is
	// the source of truth — so `go generate ./...` stays green for everyone.
	if _, err := os.Stat("testdata/scene.gltf"); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "meshgen: testdata/scene.gltf absent; keeping committed evahead_data.go")
		return
	}

	raw, err := os.ReadFile("testdata/scene.gltf")
	must(err)
	bin, err := os.ReadFile("testdata/scene.bin")
	must(err)

	var doc gltf
	must(json.Unmarshal(raw, &doc))

	readVec3 := func(accIdx int) []pt {
		a := doc.Accessors[accIdx]
		bv := doc.BufferViews[a.BufferView]
		stride := bv.ByteStride
		if stride == 0 {
			stride = 12
		}
		base := bv.ByteOffset + a.ByteOffset
		out := make([]pt, a.Count)
		for i := 0; i < a.Count; i++ {
			o := base + i*stride
			out[i].x = f32(bin, o)
			out[i].y = f32(bin, o+4)
			out[i].z = f32(bin, o+8)
		}
		return out
	}

	// Gather points per region from the 6 material submeshes.
	byRegion := map[string][]pt{}
	for _, m := range doc.Meshes {
		reg := regionFor(m.Name)
		prim := m.Primitives[0]
		pos := readVec3(prim.Attributes["POSITION"])
		nrm := readVec3(prim.Attributes["NORMAL"])
		for i := range pos {
			p := pos[i]
			if i < len(nrm) {
				p.nx, p.ny, p.nz = nrm[i].x, nrm[i].y, nrm[i].z
			}
			byRegion[reg] = append(byRegion[reg], p)
		}
	}

	// Center on the overall bounding-box midpoint, then scale to max radius 1.
	min := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	max := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, ps := range byRegion {
		for _, p := range ps {
			min[0], max[0] = mn(min[0], p.x), mx(max[0], p.x)
			min[1], max[1] = mn(min[1], p.y), mx(max[1], p.y)
			min[2], max[2] = mn(min[2], p.z), mx(max[2], p.z)
		}
	}
	cx, cy, cz := (min[0]+max[0])/2, (min[1]+max[1])/2, (min[2]+max[2])/2
	maxR := 0.0
	for _, ps := range byRegion {
		for _, p := range ps {
			d := math.Sqrt(sq(p.x-cx) + sq(p.y-cy) + sq(p.z-cz))
			maxR = mx(maxR, d)
		}
	}
	if maxR == 0 {
		maxR = 1
	}

	// Per-region voxel downsample (averaged position + normal per cell).
	type cell struct {
		sx, sy, sz, snx, sny, snz float64
		n                         int
	}
	type baked struct {
		x, y, z, nx, ny, nz float64
		region              string
	}
	var out []baked
	regions := make([]string, 0, len(byRegion))
	for r := range byRegion {
		regions = append(regions, r)
	}
	sort.Strings(regions)
	for _, r := range regions {
		cells := map[[3]int]*cell{}
		for _, p := range byRegion[r] {
			x := (p.x - cx) / maxR
			y := (p.y - cy) / maxR
			z := (p.z - cz) / maxR
			key := [3]int{int(math.Floor(x / targetVoxel)), int(math.Floor(y / targetVoxel)), int(math.Floor(z / targetVoxel))}
			c := cells[key]
			if c == nil {
				c = &cell{}
				cells[key] = c
			}
			c.sx += x
			c.sy += y
			c.sz += z
			c.snx += p.nx
			c.sny += p.ny
			c.snz += p.nz
			c.n++
		}
		keys := make([][3]int, 0, len(cells))
		for k := range cells {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, b := keys[i], keys[j]
			if a[0] != b[0] {
				return a[0] < b[0]
			}
			if a[1] != b[1] {
				return a[1] < b[1]
			}
			return a[2] < b[2]
		})
		for _, k := range keys {
			c := cells[k]
			nf := float64(c.n)
			nx, ny, nz := normalize(c.snx, c.sny, c.snz)
			out = append(out, baked{c.sx / nf, c.sy / nf, c.sz / nf, nx, ny, nz, r})
		}
		fmt.Fprintf(os.Stderr, "region %-10s raw=%-7d baked=%d\n", r, len(byRegion[r]), len(cells))
	}
	fmt.Fprintf(os.Stderr, "total baked points: %d (bbox=%.2f..%.2f / %.2f..%.2f / %.2f..%.2f, maxR=%.3f)\n",
		len(out), min[0], max[0], min[1], max[1], min[2], max[2], maxR)

	// Emit the generated Go source.
	var b strings.Builder
	b.WriteString("// Code generated by meshgen.go; DO NOT EDIT.\n")
	b.WriteString("//\n")
	b.WriteString("// EVA-01 head point cloud baked from \"Evangelion Unit 01 head Printable\"\n")
	b.WriteString("// by ngkirto (https://sketchfab.com/3d-models/evangelion-unit-01-head-printable-3a8d3a39f72740fcb08760a12f97b0a3),\n")
	b.WriteString("// licensed CC-BY-4.0 (https://creativecommons.org/licenses/by/4.0/).\n\n")
	b.WriteString("package anim\n\n")
	fmt.Fprintf(&b, "// evaHeadPoints is the decimated EVA-01 head surface (%d points).\n", len(out))
	b.WriteString("var evaHeadPoints = []bakedPoint{\n")
	for _, p := range out {
		fmt.Fprintf(&b, "\t{%s, %s, %s, %s, %s, %s, %s},\n",
			ff(p.x), ff(p.y), ff(p.z), ff(p.nx), ff(p.ny), ff(p.nz), regionConst(p.region))
	}
	b.WriteString("}\n")
	must(os.WriteFile("evahead_data.go", []byte(b.String()), 0o644))
	fmt.Fprintln(os.Stderr, "wrote evahead_data.go")
}

func regionFor(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "black eyes"):
		return "blackeyes"
	case strings.Contains(n, "eyes"):
		return "eyes"
	case strings.Contains(n, "green"):
		return "green"
	case strings.Contains(n, "red"):
		return "red"
	case strings.Contains(n, "brown"):
		return "brown"
	default:
		return "armor"
	}
}

func regionConst(r string) string {
	switch r {
	case "green":
		return "regionGreen"
	case "eyes":
		return "regionEyes"
	case "blackeyes":
		return "regionBlackEyes"
	case "red":
		return "regionRed"
	case "brown":
		return "regionBrown"
	default:
		return "regionArmor"
	}
}

func f32(b []byte, off int) float64 {
	return float64(math.Float32frombits(binary.LittleEndian.Uint32(b[off : off+4])))
}

func normalize(x, y, z float64) (float64, float64, float64) {
	m := math.Sqrt(x*x + y*y + z*z)
	if m == 0 {
		return 0, 0, 1
	}
	return x / m, y / m, z / m
}

// ff formats a float as a compact float32 literal.
func ff(v float64) string {
	return fmt.Sprintf("%.4f", float32(v))
}

func sq(v float64) float64 { return v * v }
func mn(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func mx(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "meshgen:", err)
		os.Exit(1)
	}
}
