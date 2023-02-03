package main

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/petoc/hgt"
)

var heightmap Heightmap

func main() {
	h, err := hgt.OpenDataDir("data/unzipped", nil)

	if err != nil {
		panic(err)
	}

	defer h.Close()

	heightmap = Heightmap{
		ElevationDataset: h,
	}

	router := chi.NewRouter()
	router.Get("/{z}/{x}/{y}.png", handleTile)

	http.ListenAndServe(":8000", router)
}

func handleTile(w http.ResponseWriter, r *http.Request) {
	xParam := chi.URLParam(r, "x")
	yParam := chi.URLParam(r, "y")
	zParam := chi.URLParam(r, "z")

	x, xParseErr := strconv.Atoi(xParam)
	y, yParseErr := strconv.Atoi(yParam)
	z, zParseErr := strconv.Atoi(zParam)

	if xParseErr != nil || yParseErr != nil || zParseErr != nil {
		http.Error(w, "coordinates parse error", http.StatusBadRequest)
		return
	}

	bytes, err := heightmap.GetTileHeightmap(z, x, y)

	if err != nil {
		http.Error(w, "cannot generate heightmap. "+err.Error(), http.StatusBadRequest)
		return
	}

	contentDisposition := fmt.Sprintf("inline; filename=\"%d.png\"", y)

	w.Header().Add("Content-Type", "image/png")
	w.Header().Add("Content-Disposition", contentDisposition)
	w.Write(bytes)
}
