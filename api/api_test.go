package api

import (
	"context"
	"github.com/geovannyAvelar/lukla/heightmap"
	"github.com/go-chi/chi"
	"net/http"
	"net/http/httptest"
	"testing"
)

type HeightmapGenTest struct {
}

func (h HeightmapGenTest) GetTileHeightmap(z, x, y, resolution int) ([]byte, error) {
	return []byte{}, nil
}

func (h HeightmapGenTest) CreateHeightMapImage(lat, lon float64, side int, conf heightmap.ResolutionConfig) ([]byte, error) {
	return []byte{}, nil
}

func (h HeightmapGenTest) GetPointsElevations(points []heightmap.Point) []heightmap.Point {
	return points
}

func TestHandleTile(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest("GET", "/0/0/0.png", nil)

	if err != nil {
		t.Errorf("Error creating a new request: %v", err)
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("z", "0")
	rctx.URLParams.Add("x", "0")
	rctx.URLParams.Add("y", "0")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	api := HttpApi{HeightmapGen: HeightmapGenTest{}}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(api.handleTile)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code. Expected: %d. Got: %d.", http.StatusOK, status)
	}
}

func TestHandleSquare(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest("GET", "/heightmap?lat=0.0&lon=0.0", nil)

	if err != nil {
		t.Errorf("Error creating a new request: %v", err)
	}

	rctx := chi.NewRouteContext()
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	api := HttpApi{HeightmapGen: HeightmapGenTest{}}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(api.handleSquare)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Handler returned wrong status code. Expected: %d. Got: %d.", http.StatusOK, status)
	}
}
