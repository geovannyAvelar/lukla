package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/geovannyAvelar/lukla/heightmap"
	"github.com/go-chi/chi"
)

type HeightmapGenTest struct {
	TileResult heightmap.TileGenerationResult
	TileErr    error
}

func (h HeightmapGenTest) GetTileHeightmap(ctx context.Context, z, x, y, resolution int) ([]byte, error) {
	return []byte{}, nil
}

func (h HeightmapGenTest) CreateHeightMapImage(ctx context.Context, lat, lon, side float64, conf heightmap.ResolutionConfig) ([]byte, error) {
	return []byte{}, nil
}

func (h HeightmapGenTest) GetPointsElevations(points []heightmap.Point) []heightmap.Point {
	return points
}

func (h HeightmapGenTest) GenerateAllTilesInZoomLevel(ctx context.Context, zoomLevel int) (heightmap.TileGenerationResult, error) {
	return h.TileResult, h.TileErr
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

// TestCoordinateJSONFieldNames guards against latitude/longitude being
// associated with the wrong JSON tag (regression test for a bug where the
// tags were swapped).
func TestCoordinateJSONFieldNames(t *testing.T) {
	t.Parallel()

	c := coordinate{Latitude: 27.687397, Longitude: 86.731814, Elevation: 8849}

	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("cannot marshal coordinate: %s", err)
	}

	var m map[string]float64
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("cannot unmarshal into map: %s", err)
	}

	if m["latitude"] != c.Latitude {
		t.Errorf("expected JSON field \"latitude\" to be %f, got %f", c.Latitude, m["latitude"])
	}

	if m["longitude"] != c.Longitude {
		t.Errorf("expected JSON field \"longitude\" to be %f, got %f", c.Longitude, m["longitude"])
	}
}

// TestHandleHeightmapProfilePreservesLatLon sends distinct latitude and
// longitude values through POST /heightmap/points and checks that each value
// comes back on the correct field, exercising decode -> toPoint -> encode.
func TestHandleHeightmapProfilePreservesLatLon(t *testing.T) {
	t.Parallel()

	reqBody := `[{"latitude":10.5,"longitude":20.25}]`
	req := httptest.NewRequest("POST", "/heightmap/points", strings.NewReader(reqBody))

	a := HttpApi{HeightmapGen: HeightmapGenTest{}}

	rr := httptest.NewRecorder()
	http.HandlerFunc(a.handleHeightmapProfile).ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Fatalf("handler returned wrong status code. Expected: %d. Got: %d. Body: %s",
			http.StatusOK, status, rr.Body.String())
	}

	var result []coordinate
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("cannot unmarshal response: %s", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 coordinate, got %d", len(result))
	}

	if result[0].Latitude != 10.5 {
		t.Errorf("expected latitude 10.5, got %f", result[0].Latitude)
	}

	if result[0].Longitude != 20.25 {
		t.Errorf("expected longitude 20.25, got %f", result[0].Longitude)
	}
}

func TestHandleHeightmapProfileRejectsInvalidCoordinates(t *testing.T) {
	t.Parallel()

	reqBody := `[{"latitude":200,"longitude":20.25}]`
	req := httptest.NewRequest("POST", "/heightmap/points", strings.NewReader(reqBody))

	a := HttpApi{HeightmapGen: HeightmapGenTest{}}

	rr := httptest.NewRecorder()
	http.HandlerFunc(a.handleHeightmapProfile).ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("expected status %d for out-of-range latitude, got %d", http.StatusBadRequest, status)
	}
}

func TestHandleHealth(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "/health", nil)
	a := HttpApi{HeightmapGen: HeightmapGenTest{}}

	rr := httptest.NewRecorder()
	http.HandlerFunc(a.handleHealth).ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, status)
	}

	if body := rr.Body.String(); body != `{"status":"ok"}` {
		t.Errorf("unexpected health body: %s", body)
	}
}

func TestParseSquareCoordinatesValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		lat     string
		lon     string
		wantErr bool
	}{
		{"valid", "27.687397", "86.731814", false},
		{"lat too high", "91", "0", true},
		{"lat too low", "-91", "0", true},
		{"lon too high", "0", "181", true},
		{"lon too low", "0", "-181", true},
		{"not a number", "abc", "0", true},
	}

	a := HttpApi{}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q := url.Values{"lat": {tt.lat}, "lon": {tt.lon}}
			req := httptest.NewRequest("GET", "/heightmap?"+q.Encode(), nil)

			_, _, err := a.parseSquareCoordinates(req)

			if tt.wantErr && err == nil {
				t.Errorf("expected an error for lat=%s, lon=%s, got none", tt.lat, tt.lon)
			}

			if !tt.wantErr && err != nil {
				t.Errorf("expected no error for lat=%s, lon=%s, got: %s", tt.lat, tt.lon, err)
			}
		})
	}
}

func TestParseSquareSideValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		side    string
		want    float64
		wantErr bool
	}{
		{"missing uses default", "", defaultSide, false},
		{"valid", "5000", 5000, false},
		{"zero is rejected", "0", 0, true},
		{"negative is rejected", "-1", 0, true},
		{"above limit is rejected", "999999", 0, true},
	}

	a := HttpApi{}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q := url.Values{}
			if tt.side != "" {
				q.Set("side", tt.side)
			}

			req := httptest.NewRequest("GET", "/heightmap?"+q.Encode(), nil)
			got, err := a.parseSquareSide(req)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected an error for side=%q, got none", tt.side)
				}
				return
			}

			if err != nil {
				t.Errorf("expected no error for side=%q, got: %s", tt.side, err)
			}

			if got != tt.want {
				t.Errorf("expected side %f, got %f", tt.want, got)
			}
		})
	}
}

func TestParseSquareResolutionValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		res     string
		want    int
		wantErr bool
	}{
		{"missing uses default", "", defaultResolution, false},
		{"valid", "512", 512, false},
		{"zero is rejected", "0", 0, true},
		{"negative is rejected", "-256", 0, true},
		{"above limit is rejected", "4096", 0, true},
	}

	a := HttpApi{}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q := url.Values{}
			if tt.res != "" {
				q.Set("resolution", tt.res)
			}

			req := httptest.NewRequest("GET", "/heightmap?"+q.Encode(), nil)
			got, err := a.parseSquareResolution(req)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected an error for resolution=%q, got none", tt.res)
				}
				return
			}

			if err != nil {
				t.Errorf("expected no error for resolution=%q, got: %s", tt.res, err)
			}

			if got != tt.want {
				t.Errorf("expected resolution %d, got %d", tt.want, got)
			}
		})
	}
}

func TestParseTileCoordinatesValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		z, x, y string
		wantErr bool
	}{
		{"valid at zoom 0", "0", "0", "0", false},
		{"valid at zoom 3", "3", "5", "5", false},
		{"x out of range for zoom", "3", "8", "0", true},
		{"y out of range for zoom", "3", "0", "8", true},
		{"negative zoom", "-1", "0", "0", true},
		{"zoom above limit", strconv.Itoa(maxTileZoomLevel + 1), "0", "0", true},
		{"not a number", "a", "0", "0", true},
	}

	a := HttpApi{}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/"+tt.z+"/"+tt.x+"/"+tt.y+".png", nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("z", tt.z)
			rctx.URLParams.Add("x", tt.x)
			rctx.URLParams.Add("y", tt.y)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			_, err := a.parseTileCoordinates(req)

			if tt.wantErr && err == nil {
				t.Errorf("expected an error for z=%s, x=%s, y=%s, got none", tt.z, tt.x, tt.y)
			}

			if !tt.wantErr && err != nil {
				t.Errorf("expected no error for z=%s, x=%s, y=%s, got: %s", tt.z, tt.x, tt.y, err)
			}
		})
	}
}

func TestProcessAllTilesRejectsZoomAboveLimit(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("POST", "/processTiles/"+strconv.Itoa(maxBatchZoomLevel+1), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("z", strconv.Itoa(maxBatchZoomLevel+1))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	a := HttpApi{HeightmapGen: HeightmapGenTest{}}

	rr := httptest.NewRecorder()
	http.HandlerFunc(a.processAllTiles).ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("expected status %d for zoom above limit, got %d", http.StatusBadRequest, status)
	}
}

// TestProcessAllTilesStatusReflectsBatchOutcome is a regression test for the
// endpoint always answering 200 with an empty body regardless of how many
// tiles in the batch actually failed.
func TestProcessAllTilesStatusReflectsBatchOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		result     heightmap.TileGenerationResult
		wantStatus int
	}{
		{"all succeeded", heightmap.TileGenerationResult{Total: 4, Succeeded: 4, Failed: 0}, http.StatusOK},
		{"partial failure", heightmap.TileGenerationResult{Total: 4, Succeeded: 2, Failed: 2}, http.StatusMultiStatus},
		{"all failed", heightmap.TileGenerationResult{Total: 4, Succeeded: 0, Failed: 4}, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("POST", "/processTiles/1", nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("z", "1")
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			a := HttpApi{HeightmapGen: HeightmapGenTest{TileResult: tt.result}}

			rr := httptest.NewRecorder()
			http.HandlerFunc(a.processAllTiles).ServeHTTP(rr, req)

			if status := rr.Code; status != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, status)
			}

			var got heightmap.TileGenerationResult
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatalf("cannot unmarshal response body: %s. Body: %s", err, rr.Body.String())
			}

			if got != tt.result {
				t.Errorf("expected response body %+v, got %+v", tt.result, got)
			}
		})
	}
}

// TestProcessAllTilesWritesNoBodyWhenRequestCanceled is a regression test for
// wasted work: when the client is already gone, the handler shouldn't bother
// marshaling and writing a response nobody will read.
func TestProcessAllTilesWritesNoBodyWhenRequestCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest("POST", "/processTiles/1", nil).WithContext(ctx)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("z", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	a := HttpApi{HeightmapGen: HeightmapGenTest{
		TileResult: heightmap.TileGenerationResult{Total: 4, Succeeded: 1, Failed: 0, Canceled: true},
	}}

	rr := httptest.NewRecorder()
	http.HandlerFunc(a.processAllTiles).ServeHTTP(rr, req)

	if rr.Body.Len() != 0 {
		t.Errorf("expected no response body for an already-canceled request, got: %s", rr.Body.String())
	}
}
