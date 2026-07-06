package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TODO: confirm venue coordinates. Placeholder is TAMU-CC (Islanders home).
const (
	venueLat   = 27.712194
	venueLon   = -97.326264
	geoTimeout = 5 * time.Second
	geoAPIFmt  = "http://ip-api.com/json/%s?fields=status,message,country,regionName,city,lat,lon,isp,org,as,reverse,query"
)

type IPInfo struct {
	IP       string  `json:"ip"`
	Hostname string  `json:"hostname"`
	City     string  `json:"city"`
	Region   string  `json:"region"`
	Country  string  `json:"country"`
	ISP      string  `json:"isp"`
	Org      string  `json:"org"`
	ASN      string  `json:"asn"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	Distance float64 `json:"distanceMiles"`
	At       int64   `json:"at"`
	Status   string  `json:"status"` // "ok", "private", "error"
	Error    string  `json:"error,omitempty"`
}

type geoAPIResp struct {
	Status  string  `json:"status"`
	Message string  `json:"message"`
	Country string  `json:"country"`
	Region  string  `json:"regionName"`
	City    string  `json:"city"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	ISP     string  `json:"isp"`
	Org     string  `json:"org"`
	AS      string  `json:"as"`
	Reverse string  `json:"reverse"`
	Query   string  `json:"query"`
}

var (
	currentIP   IPInfo
	currentIPMu sync.RWMutex
	geoClient   = &http.Client{Timeout: geoTimeout}
)

func getClientIP(r *http.Request) string {
	if cfIP := r.Header.Get("CF-Connecting-IP"); cfIP != "" {
		return cfIP
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// captureIP kicks off an async ip-api lookup and updates the current IPInfo.
// Safe to call from a request handler — never blocks the response.
func captureIP(ip string) {
	go func() {
		info, err := lookupIP(ip)
		if err != nil {
			log.Printf("geo lookup for %s: %v", ip, err)
			info = IPInfo{IP: ip, Status: "error", Error: err.Error(), At: time.Now().Unix()}
		}
		setCurrentIPInfo(info)
	}()
}

func lookupIP(ip string) (IPInfo, error) {
	if isPrivateOrLoopback(ip) {
		return IPInfo{IP: ip, Status: "private", At: time.Now().Unix()}, nil
	}
	resp, err := geoClient.Get(fmt.Sprintf(geoAPIFmt, ip))
	if err != nil {
		return IPInfo{}, err
	}
	defer resp.Body.Close()

	var g geoAPIResp
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		return IPInfo{}, err
	}
	if g.Status != "success" {
		return IPInfo{}, errors.New(g.Message)
	}
	return IPInfo{
		IP:       ip,
		Hostname: g.Reverse,
		City:     g.City,
		Region:   g.Region,
		Country:  g.Country,
		ISP:      g.ISP,
		Org:      g.Org,
		ASN:      g.AS,
		Lat:      g.Lat,
		Lon:      g.Lon,
		Distance: haversineMiles(venueLat, venueLon, g.Lat, g.Lon),
		At:       time.Now().Unix(),
		Status:   "ok",
	}, nil
}

func isPrivateOrLoopback(ip string) bool {
	p := net.ParseIP(ip)
	if p == nil {
		return true
	}
	return p.IsLoopback() || p.IsPrivate() || p.IsLinkLocalUnicast() || p.IsUnspecified()
}

func haversineMiles(lat1, lon1, lat2, lon2 float64) float64 {
	const earthMi = 3958.7613
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthMi * c
}

func setCurrentIPInfo(info IPInfo) {
	currentIPMu.Lock()
	currentIP = info
	currentIPMu.Unlock()
}

func getCurrentIPInfo() IPInfo {
	currentIPMu.RLock()
	defer currentIPMu.RUnlock()
	return currentIP
}
