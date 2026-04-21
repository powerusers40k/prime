package main

import (
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"golang.org/x/sync/semaphore"
)

var (
	appStyle = lipgloss.NewStyle().
			Padding(1, 2)

	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF75B7")).
			Bold(true).
			Padding(0, 1)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575")).
			Padding(0, 1)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Padding(0, 1)
)

type Node struct {
	ID        string
	Address   string
	LastSeen  time.Time
	IPs       []string
	IsActive  bool
}

type Website struct {
	Domain      string
	Name        string
	Owner       string
	HTML        string
	CSS         string
	Lua         string
	GoBackend   string
	Restricts   string
	Created     time.Time
	Updated     time.Time
	Version     int
	BackendProc *os.Process
	Port        int
}

type Video struct {
	ID          string
	Title       string
	Description string
	Filename    string
	Uploader    string
	Uploaded    time.Time
	Views       int
	Size        int64
	Hash        string
}

type Snapshot struct {
	URL         string
	Timestamp   time.Time
	HTML        string
	CSS         string
	Lua         string
	GoBackend   string
	Screenshot  []byte
	CrawledBy   string
	ContentHash string
	FullAssets  bool
}

type PeerMessage struct {
	Type      string
	From      string
	Data      []byte
	Timestamp time.Time
	TTL       int
}

type P2PNetwork struct {
	mu           sync.RWMutex
	nodeID       string
	peers        map[string]*Node
	websites     map[string]*Website
	videos       map[string]*Video
	snapshots    map[string][]Snapshot
	messages     chan PeerMessage
	ipSemaphore  *semaphore.Weighted
	localIPs     []string
	listener     net.Listener
	httpServer   *http.Server
	backendPorts map[string]int
	nextPort     int
	lanMode      bool
	wbsCSS       string
}

type Model struct {
	network       *P2PNetwork
	currentView   string
	width         int
	height        int
	ready         bool
	
	list         list.Model
	table        table.Model
	viewport     viewport.Model
	
	inputs       []textinput.Model
	focusIndex   int
	
	statusMsg    string
	errorMsg     string
	
	wbsURL       string
	wbsContent   string
	wbsHistory   []string
	wbsHistoryIdx int
	
	createDomain string
	createName   string
	createHTML   string
	createCSS    string
	createLua    string
	createGo     string
	
	uploadTitle  string
	uploadDesc   string
	uploadPath   string
	
	messages     []string
	cssEditor    textinput.Model
}

func NewP2PNetwork() *P2PNetwork {
	defaultCSS := `/* tweakui.css - WBS Interface Styling */
body {
    font-family: monospace;
    max-width: 800px;
    margin: 0 auto;
    padding: 20px;
    background: #1a1a1a;
    color: #fff;
}
.address-bar {
    width: 100%;
    padding: 10px;
    font-size: 16px;
    margin-bottom: 20px;
    background: #2a2a2a;
    color: #fff;
    border: 1px solid #ff75b7;
}
.site-list {
    display: grid;
    gap: 10px;
}
.site-item {
    padding: 15px;
    border: 1px solid #ff75b7;
    cursor: pointer;
    background: #2a2a2a;
}
.site-item:hover {
    background: #3a3a3a;
}
button {
    background: #ff75b7;
    color: #000;
    border: none;
    padding: 10px 20px;
    cursor: pointer;
    font-weight: bold;
}
.free-badge {
    color: #04b575;
    font-size: 12px;
    margin-left: 10px;
}
a {
    color: #ff75b7;
    text-decoration: none;
}
h1, h2, h3 {
    color: #ff75b7;
}
.snapshot {
    border: 1px solid #ff75b7;
    margin: 10px 0;
    padding: 15px;
    background: #2a2a2a;
}
.snapshot-header {
    cursor: pointer;
    font-weight: bold;
    color: #ff75b7;
}
.snapshot-content {
    display: none;
    margin-top: 10px;
}
.snapshot-preview {
    background: #1a1a1a;
    padding: 15px;
    margin: 10px 0;
    border: 1px solid #04b575;
}
.video-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(250px, 1fr));
    gap: 20px;
}
.video-card {
    border: 1px solid #ff75b7;
    padding: 15px;
    cursor: pointer;
    background: #2a2a2a;
}
.video-card:hover {
    background: #3a3a3a;
}
.upload-form, .crawl-form, .css-editor {
    margin: 20px 0;
    padding: 20px;
    border: 1px solid #ff75b7;
    background: #2a2a2a;
}
input, textarea, select {
    width: 100%;
    padding: 8px;
    margin: 10px 0;
    background: #1a1a1a;
    color: #fff;
    border: 1px solid #ff75b7;
    font-family: monospace;
}
input:focus, textarea:focus {
    outline: none;
    border-color: #04b575;
}
.asset-tabs {
    display: flex;
    gap: 5px;
    margin: 10px 0;
}
.asset-tab {
    padding: 5px 10px;
    background: #1a1a1a;
    border: 1px solid #ff75b7;
    cursor: pointer;
}
.asset-tab.active {
    background: #ff75b7;
    color: #000;
}
.asset-content {
    background: #1a1a1a;
    padding: 15px;
    border: 1px solid #04b575;
    max-height: 400px;
    overflow: auto;
}
pre {
    margin: 0;
    white-space: pre-wrap;
    word-wrap: break-word;
}
.timeline {
    margin: 20px 0;
}
`
	
	return &P2PNetwork{
		nodeID:       uuid.New().String()[:8],
		peers:        make(map[string]*Node),
		websites:     make(map[string]*Website),
		videos:       make(map[string]*Video),
		snapshots:    make(map[string][]Snapshot),
		messages:     make(chan PeerMessage, 1000),
		ipSemaphore:  semaphore.NewWeighted(5),
		localIPs:     make([]string, 0),
		backendPorts: make(map[string]int),
		nextPort:     9000,
		lanMode:      false,
		wbsCSS:       defaultCSS,
	}
}

func (p *P2PNetwork) Start(port int, lanMode bool) error {
	p.lanMode = lanMode
	
	os.MkdirAll("snapshots", 0755)
	p.loadSnapshots()
	
	if lanMode {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			return err
		}
		p.listener = listener
		
		ips, err := p.getLocalIPs()
		if err == nil {
			p.localIPs = ips
		}
		
		go p.listenForPeers()
		go p.broadcastPresence()
		go p.processMessages()
	}
	
	go p.startWebServers()
	
	return nil
}

func (p *P2PNetwork) loadSnapshots() {
	data, err := os.ReadFile(filepath.Join("snapshots", "snapshots.json"))
	if err == nil {
		json.Unmarshal(data, &p.snapshots)
	}
}

func (p *P2PNetwork) saveSnapshots() {
	data, _ := json.MarshalIndent(p.snapshots, "", "  ")
	os.WriteFile(filepath.Join("snapshots", "snapshots.json"), data, 0644)
}

func (p *P2PNetwork) getLocalIPs() ([]string, error) {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	
	return ips, nil
}

func (p *P2PNetwork) listenForPeers() {
	if !p.lanMode {
		return
	}
	
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			continue
		}
		go p.handlePeerConnection(conn)
	}
}

func (p *P2PNetwork) handlePeerConnection(conn net.Conn) {
	defer conn.Close()
	
	decoder := gob.NewDecoder(conn)
	var msg PeerMessage
	if err := decoder.Decode(&msg); err != nil {
		return
	}
	
	switch msg.Type {
	case "HELLO":
		var node Node
		if err := json.Unmarshal(msg.Data, &node); err == nil {
			p.mu.Lock()
			node.LastSeen = time.Now()
			node.IsActive = true
			p.peers[node.ID] = &node
			p.mu.Unlock()
			
			encoder := gob.NewEncoder(conn)
			welcome := PeerMessage{
				Type:      "WELCOME",
				From:      p.nodeID,
				Data:      []byte(`{"status":"connected"}`),
				Timestamp: time.Now(),
				TTL:       1,
			}
			encoder.Encode(welcome)
		}
		
	case "SYNC_WEBSITES":
		p.mu.RLock()
		websites := make([]Website, 0, len(p.websites))
		for _, w := range p.websites {
			websites = append(websites, *w)
		}
		p.mu.RUnlock()
		
		data, _ := json.Marshal(websites)
		encoder := gob.NewEncoder(conn)
		encoder.Encode(PeerMessage{
			Type:      "WEBSITES_DATA",
			From:      p.nodeID,
			Data:      data,
			Timestamp: time.Now(),
		})
		
	case "WEBSITES_DATA":
		var websites []Website
		if err := json.Unmarshal(msg.Data, &websites); err == nil {
			p.mu.Lock()
			for _, w := range websites {
				if existing, ok := p.websites[w.Domain]; !ok || existing.Version < w.Version {
					p.websites[w.Domain] = &w
				}
			}
			p.mu.Unlock()
		}
		
	case "SYNC_VIDEOS":
		p.mu.RLock()
		videos := make([]Video, 0, len(p.videos))
		for _, v := range p.videos {
			videos = append(videos, *v)
		}
		p.mu.RUnlock()
		
		data, _ := json.Marshal(videos)
		encoder := gob.NewEncoder(conn)
		encoder.Encode(PeerMessage{
			Type:      "VIDEOS_DATA",
			From:      p.nodeID,
			Data:      data,
			Timestamp: time.Now(),
		})
		
	case "VIDEOS_DATA":
		var videos []Video
		if err := json.Unmarshal(msg.Data, &videos); err == nil {
			p.mu.Lock()
			for _, v := range videos {
				if _, ok := p.videos[v.ID]; !ok {
					p.videos[v.ID] = &v
				}
			}
			p.mu.Unlock()
		}
	}
}

func (p *P2PNetwork) broadcastPresence() {
	if !p.lanMode {
		return
	}
	
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for range ticker.C {
		p.mu.RLock()
		peers := make([]*Node, 0, len(p.peers))
		for _, peer := range p.peers {
			peers = append(peers, peer)
		}
		p.mu.RUnlock()
		
		for _, peer := range peers {
			go func(addr string) {
				conn, err := net.Dial("tcp", addr)
				if err != nil {
					p.mu.Lock()
					if p, ok := p.peers[addr]; ok {
						p.IsActive = false
					}
					p.mu.Unlock()
					return
				}
				defer conn.Close()
				
				nodeData, _ := json.Marshal(Node{
					ID:       p.nodeID,
					Address:  p.listener.Addr().String(),
					LastSeen: time.Now(),
					IPs:      p.localIPs,
					IsActive: true,
				})
				
				encoder := gob.NewEncoder(conn)
				encoder.Encode(PeerMessage{
					Type:      "HELLO",
					From:      p.nodeID,
					Data:      nodeData,
					Timestamp: time.Now(),
					TTL:       3,
				})
			}(peer.Address)
		}
	}
}

func (p *P2PNetwork) processMessages() {
	if !p.lanMode {
		return
	}
	
	for msg := range p.messages {
		if msg.TTL <= 0 {
			continue
		}
		
		switch msg.Type {
		case "NEW_WEBSITE":
			var website Website
			if err := json.Unmarshal(msg.Data, &website); err == nil {
				p.mu.Lock()
				if existing, ok := p.websites[website.Domain]; !ok || existing.Version < website.Version {
					p.websites[website.Domain] = &website
					go p.deployWebsiteBackend(&website)
				}
				p.mu.Unlock()
			}
			
		case "NEW_VIDEO":
			var video Video
			if err := json.Unmarshal(msg.Data, &video); err == nil {
				p.mu.Lock()
				if _, ok := p.videos[video.ID]; !ok {
					p.videos[video.ID] = &video
				}
				p.mu.Unlock()
			}
		}
		
		msg.TTL--
		if msg.TTL > 0 {
			p.mu.RLock()
			for _, peer := range p.peers {
				if peer.ID != msg.From && peer.IsActive {
					go p.forwardMessage(peer.Address, msg)
				}
			}
			p.mu.RUnlock()
		}
	}
}

func (p *P2PNetwork) forwardMessage(addr string, msg PeerMessage) {
	if !p.lanMode {
		return
	}
	
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return
	}
	defer conn.Close()
	
	encoder := gob.NewEncoder(conn)
	encoder.Encode(msg)
}

func (p *P2PNetwork) startWebServers() {
	mux := http.NewServeMux()
	
	mux.HandleFunc("/", p.handleWBS)
	mux.HandleFunc("/play.vids", p.handleVids)
	mux.HandleFunc("/play.vids/upload", p.handleVideoUpload)
	mux.HandleFunc("/play.vids/watch/", p.handleVideoStream)
	mux.HandleFunc("/play.vids/stream/", p.handleVideoStream)
	mux.HandleFunc("/crawler.tri", p.handleCrawler)
	mux.HandleFunc("/crawler.tri/crawl", p.handleCrawler)
	mux.HandleFunc("/crawler.tri/snapshots/", p.handleSnapshots)
	mux.HandleFunc("/crawler.tri/preview/", p.handleSnapshotPreview)
	mux.HandleFunc("/tweakui.css", p.handleTweakUI)
	mux.HandleFunc("/tweakui.css/edit", p.handleTweakUIEdit)
	
	p.httpServer = &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}
	
	p.httpServer.ListenAndServe()
}

func (p *P2PNetwork) handleTweakUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css")
	w.Write([]byte(p.wbsCSS))
}

func (p *P2PNetwork) handleTweakUIEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		r.ParseForm()
		newCSS := r.FormValue("css")
		if newCSS != "" {
			p.mu.Lock()
			p.wbsCSS = newCSS
			p.mu.Unlock()
		}
		http.Redirect(w, r, "/tweakui.css/edit", http.StatusSeeOther)
		return
	}
	
	tmpl := `
<!DOCTYPE html>
<html>
<head>
    <title>tweakui.css - Live CSS Editor</title>
    <link rel="stylesheet" href="/tweakui.css">
    <style>
        .editor-container {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 20px;
            margin: 20px 0;
        }
        .css-input {
            width: 100%;
            height: 500px;
            font-family: monospace;
            padding: 10px;
            background: #1a1a1a;
            color: #fff;
            border: 1px solid #ff75b7;
        }
        .preview-box {
            border: 1px solid #04b575;
            padding: 20px;
            background: #2a2a2a;
            max-height: 500px;
            overflow: auto;
        }
        .preview-box h2 { margin-top: 0; }
        .status-message {
            padding: 10px;
            margin: 10px 0;
            background: #04b575;
            color: #000;
            display: none;
        }
    </style>
</head>
<body>
    <h1>tweakui.css - Live CSS Editor</h1>
    <a href="/wbs">← Back to wbs</a>
    
    <div class="status-message" id="status">CSS Updated!</div>
    
    <form method="POST" action="/tweakui.css/edit" id="cssForm">
        <div class="editor-container">
            <div>
                <h3>CSS Code</h3>
                <textarea name="css" class="css-input" id="cssInput">{{.CSS}}</textarea>
                <button type="submit">Apply CSS</button>
                <button type="button" onclick="resetCSS()">Reset to Default</button>
            </div>
            <div>
                <h3>Live Preview</h3>
                <div class="preview-box">
                    <h2>Sample Heading</h2>
                    <p>This is a preview of how elements will look with your CSS.</p>
                    <div class="site-item">Sample Site Item</div>
                    <div class="video-card">Sample Video Card</div>
                    <button>Sample Button</button>
                    <a href="#">Sample Link</a>
                    <div class="snapshot">Sample Snapshot</div>
                    <input type="text" placeholder="Sample Input" value="Sample text">
                </div>
            </div>
        </div>
    </form>
    
    <script>
        function resetCSS() {
            if (confirm('Reset to default CSS?')) {
                fetch('/tweakui.css', {
                    headers: {'Accept': 'text/css'}
                })
                .then(response => response.text())
                .then(css => {
                    document.getElementById('cssInput').value = css;
                });
            }
        }
        
        document.getElementById('cssForm').addEventListener('submit', function(e) {
            e.preventDefault();
            var formData = new FormData(this);
            fetch('/tweakui.css/edit', {
                method: 'POST',
                body: formData
            }).then(() => {
                document.getElementById('status').style.display = 'block';
                setTimeout(() => {
                    document.getElementById('status').style.display = 'none';
                }, 2000);
                document.getElementById('cssForm').submit();
            });
        });
    </script>
</body>
</html>`
	
	t := template.Must(template.New("tweakui").Parse(tmpl))
	t.Execute(w, map[string]interface{}{
		"CSS": p.wbsCSS,
	})
}

func (p *P2PNetwork) handleWBS(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	
	if path == "" || path == "wbs" {
		p.renderWBSHome(w, r)
		return
	}
	
	parts := strings.Split(path, "/")
	domain := parts[0]
	
	p.mu.RLock()
	website, exists := p.websites[domain]
	p.mu.RUnlock()
	
	if !exists {
		if domain == "play.vids" {
			p.handleVids(w, r)
			return
		}
		if domain == "crawler.tri" {
			p.handleCrawler(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}
	
	if !p.checkRestricts(website, r) {
		http.Error(w, "Access denied by restricts.txt", http.StatusForbidden)
		return
	}
	
	if website.GoBackend != "" {
		p.proxyBackendRequest(website, w, r)
	} else {
		p.serveStaticWebsite(website, w, r)
	}
}

func (p *P2PNetwork) renderWBSHome(w http.ResponseWriter, r *http.Request) {
	tmpl := `
<!DOCTYPE html>
<html>
<head>
    <title>wbs - Web Browser System</title>
    <link rel="stylesheet" href="/tweakui.css">
</head>
<body>
    <h1>wbs - Prime Network Browser</h1>
    <input type="text" class="address-bar" placeholder="Enter any domain (e.g., mysite.banana or hi.how.are.you.hey.yo)" id="urlInput">
    <button onclick="navigate()">Go</button>
    <h2>Available Sites: <span class="free-badge">ALL DOMAINS FREE</span></h2>
    <div class="site-list">
        {{range .Websites}}
        <div class="site-item" onclick="location.href='/{{.Domain}}'">
            <strong>{{.Name}}</strong> - {{.Domain}}
        </div>
        {{end}}
        <div class="site-item" onclick="location.href='/play.vids'">
            <strong>Play.vids</strong> - Video sharing platform
        </div>
        <div class="site-item" onclick="location.href='/crawler.tri'">
            <strong>Crawler.tri</strong> - Web archive crawler
        </div>
        <div class="site-item" onclick="location.href='/tweakui.css/edit'">
            <strong>tweakui.css</strong> - Live CSS Editor
        </div>
    </div>
    <script>
        function navigate() {
            var url = document.getElementById('urlInput').value;
            if (url) location.href = '/' + url;
        }
    </script>
</body>
</html>`
	
	p.mu.RLock()
	websites := make([]*Website, 0, len(p.websites))
	for _, w := range p.websites {
		websites = append(websites, w)
	}
	p.mu.RUnlock()
	
	t := template.Must(template.New("home").Parse(tmpl))
	t.Execute(w, map[string]interface{}{
		"Websites": websites,
	})
}

func (p *P2PNetwork) handleVids(w http.ResponseWriter, r *http.Request) {
	tmpl := `
<!DOCTYPE html>
<html>
<head>
    <title>play.vids - Video Platform</title>
    <link rel="stylesheet" href="/tweakui.css">
</head>
<body>
    <h1>play.vids</h1>
    <a href="/wbs">← Back to wbs</a>
    
    <div class="upload-form">
        <h2>Upload Video (Free, No Ads)</h2>
        <form action="/play.vids/upload" method="POST" enctype="multipart/form-data">
            <input type="text" name="title" placeholder="Video Title" required>
            <textarea name="description" placeholder="Description" rows="3"></textarea>
            <input type="file" name="video" accept="video/*" required>
            <button type="submit">Upload Video</button>
        </form>
    </div>
    
    <h2>Videos</h2>
    <div class="video-grid">
        {{range .Videos}}
        <div class="video-card" onclick="location.href='/play.vids/watch/{{.ID}}'">
            <h3>{{.Title}}</h3>
            <p>{{.Description}}</p>
            <small>Uploaded: {{.Uploaded.Format "2006-01-02 15:04"}}</small><br>
            <small>Views: {{.Views}}</small>
        </div>
        {{end}}
    </div>
</body>
</html>`
	
	if r.URL.Path == "/play.vids/upload" && r.Method == "POST" {
		p.handleVideoUpload(w, r)
		return
	}
	
	p.mu.RLock()
	videos := make([]*Video, 0, len(p.videos))
	for _, v := range p.videos {
		videos = append(videos, v)
	}
	sort.Slice(videos, func(i, j int) bool {
		return videos[i].Uploaded.After(videos[j].Uploaded)
	})
	p.mu.RUnlock()
	
	t := template.Must(template.New("vids").Parse(tmpl))
	t.Execute(w, map[string]interface{}{
		"Videos": videos,
	})
}

func (p *P2PNetwork) handleVideoUpload(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(100 << 20)
	
	file, header, err := r.FormFile("video")
	if err != nil {
		http.Error(w, "Failed to upload video", http.StatusBadRequest)
		return
	}
	defer file.Close()
	
	os.MkdirAll("videos", 0755)
	
	videoID := uuid.New().String()
	filename := videoID + "_" + header.Filename
	filepath := filepath.Join("videos", filename)
	
	f, err := os.Create(filepath)
	if err != nil {
		http.Error(w, "Failed to save video", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	
	hasher := sha256.New()
	multiWriter := io.MultiWriter(f, hasher)
	size, _ := io.Copy(multiWriter, file)
	
	video := &Video{
		ID:          videoID,
		Title:       r.FormValue("title"),
		Description: r.FormValue("description"),
		Filename:    filename,
		Uploader:    p.nodeID,
		Uploaded:    time.Now(),
		Views:       0,
		Size:        size,
		Hash:        hex.EncodeToString(hasher.Sum(nil)),
	}
	
	p.mu.Lock()
	p.videos[videoID] = video
	p.mu.Unlock()
	
	if p.lanMode {
		videoData, _ := json.Marshal(video)
		p.messages <- PeerMessage{
			Type:      "NEW_VIDEO",
			From:      p.nodeID,
			Data:      videoData,
			Timestamp: time.Now(),
			TTL:       5,
		}
	}
	
	http.Redirect(w, r, "/play.vids", http.StatusSeeOther)
}

func (p *P2PNetwork) handleVideoStream(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	
	if strings.HasPrefix(path, "/play.vids/stream/") {
		videoID := strings.TrimPrefix(path, "/play.vids/stream/")
		p.mu.RLock()
		video, exists := p.videos[videoID]
		p.mu.RUnlock()
		
		if !exists {
			http.NotFound(w, r)
			return
		}
		
		videoPath := filepath.Join("videos", video.Filename)
		http.ServeFile(w, r, videoPath)
		return
	}
	
	videoID := strings.TrimPrefix(path, "/play.vids/watch/")
	
	p.mu.RLock()
	video, exists := p.videos[videoID]
	p.mu.RUnlock()
	
	if !exists {
		http.NotFound(w, r)
		return
	}
	
	video.Views++
	
	tmpl := `
<!DOCTYPE html>
<html>
<head>
    <title>{{.Title}} - play.vids</title>
    <link rel="stylesheet" href="/tweakui.css">
</head>
<body>
    <a href="/play.vids">← Back to Videos</a>
    <h1>{{.Title}}</h1>
    <video controls autoplay>
        <source src="/play.vids/stream/{{.ID}}" type="video/mp4">
    </video>
    <div class="info">
        <p>{{.Description}}</p>
        <small>Uploaded: {{.Uploaded.Format "2006-01-02 15:04"}}</small><br>
        <small>Views: {{.Views}}</small><br>
        <small>Size: {{.Size}} bytes</small>
    </div>
</body>
</html>`
	
	t := template.Must(template.New("watch").Parse(tmpl))
	t.Execute(w, video)
}

func (p *P2PNetwork) handleCrawler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/crawler.tri/crawl" && r.Method == "POST" {
		url := r.FormValue("url")
		go p.crawlWebsite(url, p.nodeID)
		http.Redirect(w, r, "/crawler.tri", http.StatusSeeOther)
		return
	}
	
	tmpl := `
<!DOCTYPE html>
<html>
<head>
    <title>crawler.tri - Web Archive</title>
    <link rel="stylesheet" href="/tweakui.css">
</head>
<body>
    <h1>crawler.tri - Time Travel Web</h1>
    <a href="/wbs">← Back to wbs</a>
    
    <div class="crawl-form">
        <h2>Crawl Any Website (Saves Full Assets)</h2>
        <form action="/crawler.tri/crawl" method="POST">
            <input type="text" name="url" placeholder="Enter URL to crawl (e.g., example.banana)" required>
            <button type="submit">Crawl & Save All Assets</button>
        </form>
    </div>
    
    <h2>Recent Snapshots (Full Website Archives)</h2>
    <div class="timeline">
        {{range $url, $snapshots := .Snapshots}}
        <div class="snapshot">
            <div class="snapshot-header" onclick="toggleSnapshot(this)">
                📸 {{$url}} - {{len $snapshots}} snapshots (HTML, CSS, Lua, Go)
            </div>
            <div class="snapshot-content">
                {{range $index, $snapshot := $snapshots}}
                <div style="margin: 10px 0; padding: 10px; background: #1a1a1a;">
                    <strong>{{$snapshot.Timestamp.Format "2006-01-02 15:04:05"}}</strong>
                    <a href="/crawler.tri/preview/{{$url}}?time={{$snapshot.Timestamp.Unix}}">Preview Full Site</a>
                    <a href="/crawler.tri/snapshots/{{$url}}?time={{$snapshot.Timestamp.Unix}}">View HTML</a>
                    <small>Hash: {{$snapshot.ContentHash}}</small>
                    {{if $snapshot.FullAssets}}
                    <span style="color: #04b575;">✓ Full assets saved</span>
                    {{end}}
                </div>
                {{end}}
            </div>
        </div>
        {{end}}
    </div>
    
    <script>
        function toggleSnapshot(header) {
            var content = header.nextElementSibling;
            if (content.style.display === "none" || content.style.display === "") {
                content.style.display = "block";
            } else {
                content.style.display = "none";
            }
        }
    </script>
</body>
</html>`
	
	p.mu.RLock()
	snapshots := make(map[string][]Snapshot)
	for url, snaps := range p.snapshots {
		snapshots[url] = snaps
	}
	p.mu.RUnlock()
	
	t := template.Must(template.New("crawler").Parse(tmpl))
	t.Execute(w, map[string]interface{}{
		"Snapshots": snapshots,
	})
}

func (p *P2PNetwork) crawlWebsite(targetURL string, crawler string) {
	client := &http.Client{Timeout: 30 * time.Second}
	
	if !strings.HasPrefix(targetURL, "http") {
		targetURL = "http://" + targetURL
	}
	
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return
	}
	
	domain := parsedURL.Hostname()
	
	if !p.checkRestrictsForCrawler(domain) {
		return
	}
	
	p.mu.RLock()
	website, exists := p.websites[domain]
	p.mu.RUnlock()
	
	var html, css, lua, goBackend string
	fullAssets := false
	
	if exists {
		html = website.HTML
		css = website.CSS
		lua = website.Lua
		goBackend = website.GoBackend
		fullAssets = true
	} else {
		resp, err := client.Get(targetURL)
		if err != nil {
			if !strings.HasPrefix(targetURL, "http://localhost:8080") {
				resp, err = client.Get("http://localhost:8080/" + domain)
			}
			if err != nil {
				return
			}
		}
		defer resp.Body.Close()
		
		content, err := io.ReadAll(resp.Body)
		if err != nil {
			return
		}
		html = string(content)
	}
	
	hasher := sha256.New()
	hasher.Write([]byte(html + css + lua + goBackend))
	contentHash := hex.EncodeToString(hasher.Sum(nil))
	
	snapshot := Snapshot{
		URL:         targetURL,
		Timestamp:   time.Now(),
		HTML:        html,
		CSS:         css,
		Lua:         lua,
		GoBackend:   goBackend,
		CrawledBy:   crawler,
		ContentHash: contentHash,
		FullAssets:  fullAssets,
	}
	
	p.mu.Lock()
	p.snapshots[targetURL] = append(p.snapshots[targetURL], snapshot)
	
	if len(p.snapshots[targetURL]) > 100 {
		p.snapshots[targetURL] = p.snapshots[targetURL][1:]
	}
	p.mu.Unlock()
	
	p.saveSnapshots()
}

func (p *P2PNetwork) handleSnapshotPreview(w http.ResponseWriter, r *http.Request) {
	urlPath := strings.TrimPrefix(r.URL.Path, "/crawler.tri/preview/")
	timeStr := r.URL.Query().Get("time")
	tab := r.URL.Query().Get("tab")
	
	p.mu.RLock()
	snapshots, exists := p.snapshots[urlPath]
	p.mu.RUnlock()
	
	if !exists || len(snapshots) == 0 {
		http.NotFound(w, r)
		return
	}
	
	var targetSnapshot Snapshot
	if timeStr != "" {
		var targetUnix int64
		fmt.Sscanf(timeStr, "%d", &targetUnix)
		targetTime := time.Unix(targetUnix, 0)
		
		for _, snap := range snapshots {
			if snap.Timestamp.Unix() == targetTime.Unix() {
				targetSnapshot = snap
				break
			}
		}
	}
	
	if targetSnapshot.Timestamp.IsZero() {
		latestTime := time.Time{}
		for _, snap := range snapshots {
			if snap.Timestamp.After(latestTime) {
				targetSnapshot = snap
				latestTime = snap.Timestamp
			}
		}
	}
	
	if tab == "raw" {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(targetSnapshot.HTML))
		return
	}
	
	tmpl := `
<!DOCTYPE html>
<html>
<head>
    <title>Snapshot Preview - {{.URL}}</title>
    <link rel="stylesheet" href="/tweakui.css">
    <style>
        .preview-container {
            border: 1px solid #04b575;
            padding: 20px;
            background: #1a1a1a;
            margin: 20px 0;
        }
        .asset-panel {
            background: #2a2a2a;
            padding: 15px;
            border: 1px solid #ff75b7;
        }
        .tab-content {
            display: none;
        }
        .tab-content.active {
            display: block;
        }
        code {
            background: #1a1a1a;
            padding: 2px 5px;
            border: 1px solid #ff75b7;
        }
    </style>
</head>
<body>
    <a href="/crawler.tri">← Back to Crawler</a>
    <h1>Website Snapshot: {{.URL}}</h1>
    <p>Captured: {{.Timestamp.Format "2006-01-02 15:04:05"}} | Hash: {{.ContentHash}}</p>
    
    <div class="asset-tabs">
        <div class="asset-tab active" onclick="showTab('preview')">Live Preview</div>
        <div class="asset-tab" onclick="showTab('html')">HTML</div>
        {{if .CSS}}<div class="asset-tab" onclick="showTab('css')">CSS</div>{{end}}
        {{if .Lua}}<div class="asset-tab" onclick="showTab('lua')">Lua</div>{{end}}
        {{if .GoBackend}}<div class="asset-tab" onclick="showTab('go')">Go Backend</div>{{end}}
    </div>
    
    <div id="preview" class="tab-content active">
        <div class="preview-container">
            <iframe srcdoc="{{.HTMLEscaped}}" style="width: 100%; height: 500px; border: none; background: #fff;"></iframe>
        </div>
    </div>
    
    <div id="html" class="tab-content">
        <div class="asset-panel">
            <h3>HTML Source</h3>
            <pre><code>{{.HTML}}</code></pre>
        </div>
    </div>
    
    {{if .CSS}}
    <div id="css" class="tab-content">
        <div class="asset-panel">
            <h3>CSS Source</h3>
            <pre><code>{{.CSS}}</code></pre>
        </div>
    </div>
    {{end}}
    
    {{if .Lua}}
    <div id="lua" class="tab-content">
        <div class="asset-panel">
            <h3>Lua Script</h3>
            <pre><code>{{.Lua}}</code></pre>
        </div>
    </div>
    {{end}}
    
    {{if .GoBackend}}
    <div id="go" class="tab-content">
        <div class="asset-panel">
            <h3>Go Backend Code</h3>
            <pre><code>{{.GoBackend}}</code></pre>
        </div>
    </div>
    {{end}}
    
    <script>
        function showTab(tabId) {
            document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));
            document.querySelectorAll('.asset-tab').forEach(t => t.classList.remove('active'));
            document.getElementById(tabId).classList.add('active');
            event.target.classList.add('active');
        }
    </script>
</body>
</html>`
	
	escapedHTML := template.HTMLEscapeString(targetSnapshot.HTML)
	
	t := template.Must(template.New("preview").Parse(tmpl))
	t.Execute(w, map[string]interface{}{
		"URL":         targetSnapshot.URL,
		"Timestamp":   targetSnapshot.Timestamp,
		"ContentHash": targetSnapshot.ContentHash,
		"HTML":        targetSnapshot.HTML,
		"HTMLEscaped": escapedHTML,
		"CSS":         targetSnapshot.CSS,
		"Lua":         targetSnapshot.Lua,
		"GoBackend":   targetSnapshot.GoBackend,
	})
}

func (p *P2PNetwork) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	urlPath := strings.TrimPrefix(r.URL.Path, "/crawler.tri/snapshots/")
	timeStr := r.URL.Query().Get("time")
	
	p.mu.RLock()
	snapshots, exists := p.snapshots[urlPath]
	p.mu.RUnlock()
	
	if !exists || len(snapshots) == 0 {
		http.NotFound(w, r)
		return
	}
	
	if timeStr != "" {
		var targetUnix int64
		fmt.Sscanf(timeStr, "%d", &targetUnix)
		targetTime := time.Unix(targetUnix, 0)
		
		for _, snap := range snapshots {
			if snap.Timestamp.Unix() == targetTime.Unix() {
				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte(fmt.Sprintf("<!-- Snapshot from %s -->\n%s", 
					snap.Timestamp.Format("2006-01-02 15:04:05"), snap.HTML)))
				return
			}
		}
	}
	
	var latest Snapshot
	latestTime := time.Time{}
	for _, snap := range snapshots {
		if snap.Timestamp.After(latestTime) {
			latest = snap
			latestTime = snap.Timestamp
		}
	}
	
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(fmt.Sprintf("<!-- Latest snapshot from %s -->\n%s", 
		latest.Timestamp.Format("2006-01-02 15:04:05"), latest.HTML)))
}

func (p *P2PNetwork) checkRestricts(website *Website, r *http.Request) bool {
	if website.Restricts == "" {
		return true
	}
	
	rules := strings.Split(website.Restricts, "\n")
	clientIP := strings.Split(r.RemoteAddr, ":")[0]
	
	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		if rule == "" || strings.HasPrefix(rule, "#") {
			continue
		}
		
		parts := strings.Fields(rule)
		if len(parts) < 2 {
			continue
		}
		
		switch parts[0] {
		case "Allow":
			if parts[1] == clientIP || parts[1] == "*" {
				return true
			}
		case "Disallow":
			if parts[1] == clientIP || parts[1] == "*" {
				return false
			}
		}
	}
	
	return true
}

func (p *P2PNetwork) checkRestrictsForCrawler(domain string) bool {
	p.mu.RLock()
	website, exists := p.websites[domain]
	p.mu.RUnlock()
	
	if !exists {
		return true
	}
	
	if website.Restricts == "" {
		return true
	}
	
	return !strings.Contains(website.Restricts, "User-agent: * Disallow")
}

func (p *P2PNetwork) serveStaticWebsite(website *Website, w http.ResponseWriter, r *http.Request) {
	html := website.HTML
	if website.CSS != "" {
		html = strings.Replace(html, "</head>", "<style>"+website.CSS+"</style></head>", 1)
	}
	if website.Lua != "" {
		html = strings.Replace(html, "</body>", "<script type=\"text/lua\">"+website.Lua+"</script></body>", 1)
	}
	
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

func (p *P2PNetwork) deployWebsiteBackend(website *Website) {
	if website.GoBackend == "" {
		return
	}
	
	p.mu.Lock()
	port := p.nextPort
	p.nextPort++
	website.Port = port
	p.backendPorts[website.Domain] = port
	p.mu.Unlock()
	
	go func() {
		tmpFile := filepath.Join(os.TempDir(), website.Domain+".go")
		os.WriteFile(tmpFile, []byte(website.GoBackend), 0644)
		
		cmd := exec.Command("go", "run", tmpFile)
		cmd.Env = append(os.Environ(), fmt.Sprintf("PORT=%d", port))
		
		if err := cmd.Start(); err == nil {
			website.BackendProc = cmd.Process
			cmd.Wait()
		}
	}()
}

func (p *P2PNetwork) proxyBackendRequest(website *Website, w http.ResponseWriter, r *http.Request) {
	proxyURL := fmt.Sprintf("http://localhost:%d%s", website.Port, r.URL.Path)
	if r.URL.RawQuery != "" {
		proxyURL += "?" + r.URL.RawQuery
	}
	
	req, err := http.NewRequest(r.Method, proxyURL, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	req.Header = r.Header
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func InitialModel() Model {
	network := NewP2PNetwork()
	
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(lipgloss.Color("#FF75B7"))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(lipgloss.Color("#FF75B7"))
	
	menuItems := []list.Item{
		MenuItem{title: "wbs Browser", desc: "Browse websites with any domain (FREE)"},
		MenuItem{title: "mkwbs Creator", desc: "Create websites with any domain (FREE)"},
		MenuItem{title: "play.vids", desc: "Upload and watch videos (No ads)"},
		MenuItem{title: "crawler.tri", desc: "Full website archiving & preview"},
		MenuItem{title: "tweakui.css", desc: "Live CSS editor for wbs interface"},
		MenuItem{title: "Network Mode", desc: "Toggle LAN/Solo mode"},
		MenuItem{title: "My Websites", desc: "Manage your created websites"},
	}
	
	listModel := list.New(menuItems, delegate, 0, 0)
	listModel.Title = "Prime Network"
	listModel.Styles.Title = titleStyle
	
	columns := []table.Column{
		{Title: "Domain", Width: 30},
		{Title: "Name", Width: 20},
		{Title: "Created", Width: 20},
	}
	
	tableModel := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(15),
	)
	
	viewportModel := viewport.New(80, 20)
	viewportModel.Style = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#FF75B7"))
	
	inputs := make([]textinput.Model, 6)
	placeholders := []string{
		"Any domain (e.g., mysite.banana or hi.how.are.you.hey.yo)",
		"Website Name",
		"HTML Content",
		"CSS (optional)",
		"Lua Script (optional)",
		"Go Backend Code (optional)",
	}
	
	for i := range inputs {
		input := textinput.New()
		input.Placeholder = placeholders[i]
		if i >= 2 {
			input.CharLimit = 10000
		} else {
			input.CharLimit = 100
		}
		inputs[i] = input
	}
	
	cssEditor := textinput.New()
	cssEditor.Placeholder = "Enter CSS code..."
	cssEditor.CharLimit = 10000
	
	return Model{
		network:      network,
		currentView:  "menu",
		list:         listModel,
		table:        tableModel,
		viewport:     viewportModel,
		inputs:       inputs,
		focusIndex:   0,
		statusMsg:    "Ready - Solo Mode (No IP required)",
		wbsHistory:   []string{},
		messages:     []string{},
		cssEditor:    cssEditor,
	}
}

type MenuItem struct {
	title, desc string
}

func (i MenuItem) Title() string       { return i.title }
func (i MenuItem) Description() string { return i.desc }
func (i MenuItem) FilterValue() string { return i.title }

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		func() tea.Msg {
			m.network.Start(9000, false)
			return nil
		},
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		
		if !m.ready {
			m.list.SetSize(msg.Width-4, msg.Height-8)
			m.table.SetWidth(msg.Width - 4)
			m.table.SetHeight(msg.Height - 10)
			m.viewport.Width = msg.Width - 4
			m.viewport.Height = msg.Height - 10
			m.ready = true
		}
		
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
			
		case "q":
			if m.currentView == "menu" {
				return m, tea.Quit
			}
			m.currentView = "menu"
			m.updateMenuList()
			return m, nil
			
		case "esc":
			if m.currentView != "menu" {
				m.currentView = "menu"
				m.updateMenuList()
				return m, nil
			}
			
		case "enter":
			return m.handleEnter()
			
		case "tab":
			if m.currentView == "mkwbs" {
				m.focusIndex = (m.focusIndex + 1) % len(m.inputs)
				for i := range m.inputs {
					if i == m.focusIndex {
						cmd = m.inputs[i].Focus()
					} else {
						m.inputs[i].Blur()
					}
				}
			}
		}
	}
	
	switch m.currentView {
	case "menu":
		m.list, cmd = m.list.Update(msg)
		
	case "wbs":
		m.viewport, cmd = m.viewport.Update(msg)
		
	case "mkwbs":
		for i := range m.inputs {
			if i == m.focusIndex {
				m.inputs[i], cmd = m.inputs[i].Update(msg)
			} else {
				m.inputs[i].Blur()
			}
		}
		
	case "websites":
		m.table, cmd = m.table.Update(msg)
		
	case "tweakui":
		m.cssEditor, cmd = m.cssEditor.Update(msg)
	}
	
	return m, cmd
}

func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.currentView {
	case "menu":
		selected := m.list.SelectedItem().(MenuItem)
		switch selected.title {
		case "wbs Browser":
			m.currentView = "wbs"
			m.wbsURL = "wbs"
			m.loadWBSContent()
			m.viewport.SetContent(m.wbsContent)
			
		case "mkwbs Creator":
			m.currentView = "mkwbs"
			for i := range m.inputs {
				m.inputs[i].SetValue("")
			}
			m.focusIndex = 0
			m.inputs[0].Focus()
			
		case "play.vids":
			m.currentView = "wbs"
			m.wbsURL = "play.vids"
			m.loadWBSContent()
			m.viewport.SetContent(m.wbsContent)
			
		case "crawler.tri":
			m.currentView = "wbs"
			m.wbsURL = "crawler.tri"
			m.loadWBSContent()
			m.viewport.SetContent(m.wbsContent)
			
		case "tweakui.css":
			m.currentView = "wbs"
			m.wbsURL = "tweakui.css/edit"
			m.loadWBSContent()
			m.viewport.SetContent(m.wbsContent)
			
		case "Network Mode":
			m.network.lanMode = !m.network.lanMode
			if m.network.lanMode {
				m.statusMsg = "LAN Mode Enabled"
				m.network.Start(9000, true)
			} else {
				m.statusMsg = "Solo Mode - No IP Required"
			}
			m.updateMenuList()
			
		case "My Websites":
			m.currentView = "websites"
			m.updateWebsitesTable()
		}
		
	case "mkwbs":
		return m.createWebsite()
		
	case "websites":
		if len(m.table.Rows()) > 0 {
			row := m.table.SelectedRow()
			if len(row) > 0 {
				m.currentView = "wbs"
				m.wbsURL = row[0]
				m.loadWBSContent()
				m.viewport.SetContent(m.wbsContent)
			}
		}
	}
	
	return m, nil
}

func (m *Model) loadWBSContent() {
	if m.wbsURL == "wbs" {
		m.network.mu.RLock()
		content := "wbs - Web Browser System\n\n"
		content += "Any domain works! Create any TLD (.banana, .anything) and unlimited subdomains!\n\n"
		content += "Available Websites:\n"
		for domain, website := range m.network.websites {
			content += fmt.Sprintf("  • %s - %s\n", domain, website.Name)
		}
		content += "\nBuilt-in Sites:\n"
		content += "  • play.vids - Video platform (No ads)\n"
		content += "  • crawler.tri - Full website archiving with preview\n"
		content += "  • tweakui.css - Live CSS editor\n"
		content += "\nAll domains are FREE to create!"
		m.network.mu.RUnlock()
		m.wbsContent = content
	} else if m.wbsURL == "play.vids" {
		m.network.mu.RLock()
		content := "play.vids - Video Platform\n\n"
		content += "Free video hosting, no ads!\n\n"
		content += "Videos:\n"
		for _, video := range m.network.videos {
			content += fmt.Sprintf("  • %s - %s (Views: %d)\n", video.Title, video.Description, video.Views)
		}
		m.network.mu.RUnlock()
		m.wbsContent = content
	} else if m.wbsURL == "crawler.tri" {
		m.network.mu.RLock()
		content := "crawler.tri - Full Website Archiver\n\n"
		content += "Saves complete websites including HTML, CSS, Lua, and Go backend!\n"
		content += "Preview snapshots with all assets intact.\n\n"
		content += "Recent Snapshots:\n"
		for url, snapshots := range m.network.snapshots {
			if len(snapshots) > 0 {
				latest := snapshots[len(snapshots)-1]
				assetStatus := ""
				if latest.FullAssets {
					assetStatus = " [Full]"
				}
				content += fmt.Sprintf("  • %s - %s%s\n", url, latest.Timestamp.Format("2006-01-02 15:04"), assetStatus)
			}
		}
		m.network.mu.RUnlock()
		m.wbsContent = content
	} else if m.wbsURL == "tweakui.css/edit" {
		m.network.mu.RLock()
		content := "tweakui.css - Live CSS Editor\n\n"
		content += "Edit the CSS for the entire wbs interface!\n"
		content += "Changes apply immediately to all pages.\n\n"
		content += "Current CSS length: " + fmt.Sprintf("%d", len(m.network.wbsCSS)) + " characters"
		m.network.mu.RUnlock()
		m.wbsContent = content
	} else {
		m.network.mu.RLock()
		website, exists := m.network.websites[m.wbsURL]
		m.network.mu.RUnlock()
		
		if exists {
			m.wbsContent = fmt.Sprintf("Website: %s\nDomain: %s\n\n%s",
				website.Name, website.Domain, website.HTML)
		} else {
			m.wbsContent = fmt.Sprintf("Website not found: %s\n\nYou can create it for FREE using mkwbs!", m.wbsURL)
		}
	}
	
	m.viewport.SetContent(m.wbsContent)
	m.viewport.GotoTop()
}

func (m *Model) createWebsite() (tea.Model, tea.Cmd) {
	domain := m.inputs[0].Value()
	name := m.inputs[1].Value()
	html := m.inputs[2].Value()
	css := m.inputs[3].Value()
	lua := m.inputs[4].Value()
	goBackend := m.inputs[5].Value()
	
	if domain == "" || name == "" || html == "" {
		m.errorMsg = "Domain, name, and HTML are required"
		return m, nil
	}
	
	website := &Website{
		Domain:    domain,
		Name:      name,
		Owner:     m.network.nodeID,
		HTML:      html,
		CSS:       css,
		Lua:       lua,
		GoBackend: goBackend,
		Restricts: "",
		Created:   time.Now(),
		Updated:   time.Now(),
		Version:   1,
	}
	
	m.network.mu.Lock()
	m.network.websites[domain] = website
	m.network.mu.Unlock()
	
	if goBackend != "" {
		go m.network.deployWebsiteBackend(website)
	}
	
	if m.network.lanMode {
		websiteData, _ := json.Marshal(website)
		m.network.messages <- PeerMessage{
			Type:      "NEW_WEBSITE",
			From:      m.network.nodeID,
			Data:      websiteData,
			Timestamp: time.Now(),
			TTL:       5,
		}
	}
	
	m.statusMsg = fmt.Sprintf("Website created: %s (FREE)", domain)
	m.currentView = "menu"
	m.updateMenuList()
	
	return m, nil
}

func (m *Model) updateMenuList() {
	mode := "Solo (No IP)"
	if m.network.lanMode {
		mode = fmt.Sprintf("LAN (%d peers)", len(m.network.peers))
	}
	
	items := []list.Item{
		MenuItem{title: "wbs Browser", desc: "Browse websites with any domain (FREE)"},
		MenuItem{title: "mkwbs Creator", desc: "Create websites with any domain (FREE)"},
		MenuItem{title: "play.vids", desc: "Upload and watch videos (No ads)"},
		MenuItem{title: "crawler.tri", desc: "Full website archiving & preview"},
		MenuItem{title: "tweakui.css", desc: "Live CSS editor for wbs interface"},
		MenuItem{title: "Network Mode", desc: mode},
		MenuItem{title: "My Websites", desc: fmt.Sprintf("%d websites", len(m.network.websites))},
	}
	m.list.SetItems(items)
}

func (m *Model) updateWebsitesTable() {
	m.network.mu.RLock()
	defer m.network.mu.RUnlock()
	
	rows := []table.Row{}
	for _, website := range m.network.websites {
		rows = append(rows, table.Row{
			website.Domain,
			website.Name,
			website.Created.Format("2006-01-02 15:04"),
		})
	}
	
	m.table.SetRows(rows)
}

func (m Model) View() string {
	if !m.ready {
		return "Initializing Prime Network...\n"
	}
	
	var content string
	
	switch m.currentView {
	case "menu":
		content = appStyle.Render(m.list.View())
		
	case "wbs":
		urlBar := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#FF75B7")).
			Padding(0, 1).
			Render(fmt.Sprintf("🌐 wbs://%s", m.wbsURL))
		
		help := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262")).
			Render("Q Menu • Any domain works!")
		
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			urlBar,
			m.viewport.View(),
			help,
		)
		
	case "mkwbs":
		var builder strings.Builder
		builder.WriteString(titleStyle.Render("mkwbs - Create Website (FREE)") + "\n\n")
		builder.WriteString("Any domain, any TLD, unlimited subdomains!\n\n")
		
		labels := []string{"Domain:", "Name:", "HTML:", "CSS:", "Lua:", "Go Backend:"}
		for i, input := range m.inputs {
			builder.WriteString(fmt.Sprintf("%s\n", labels[i]))
			builder.WriteString(input.View())
			builder.WriteString("\n\n")
		}
		
		help := "\nTab: Switch • Enter: Create • Esc: Cancel"
		builder.WriteString(help)
		
		content = appStyle.Render(builder.String())
		
	case "websites":
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			titleStyle.Render("My Websites (All FREE)"),
			m.table.View(),
			"Enter: Open • Q: Back",
		)
		
	default:
		content = "Unknown view"
	}
	
	statusBar := m.renderStatusBar()
	
	return lipgloss.JoinVertical(
		lipgloss.Left,
		content,
		statusBar,
	)
}

func (m Model) renderStatusBar() string {
	status := statusStyle.Render(fmt.Sprintf("● %s", m.statusMsg))
	
	if m.errorMsg != "" {
		status = errorStyle.Render(fmt.Sprintf("⚠ %s", m.errorMsg))
		m.errorMsg = ""
	}
	
	mode := "Solo"
	if m.network.lanMode {
		mode = fmt.Sprintf("LAN | Peers: %d", len(m.network.peers))
	}
	
	networkInfo := fmt.Sprintf("Node: %s | Mode: %s | Websites: %d | Snapshots: %d",
		m.network.nodeID,
		mode,
		len(m.network.websites),
		len(m.network.snapshots))
	
	bar := lipgloss.JoinHorizontal(
		lipgloss.Top,
		status,
		lipgloss.NewStyle().
			Foreground(lipgloss.Color("#626262")).
			Padding(0, 2).
			Render(networkInfo),
	)
	
	return lipgloss.NewStyle().
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#FF75B7")).
		Padding(0, 1).
		Width(m.width - 4).
		Render(bar)
}

func main() {
	os.MkdirAll("videos", 0755)
	os.MkdirAll("websites", 0755)
	os.MkdirAll("snapshots", 0755)
	
	p := tea.NewProgram(InitialModel(), tea.WithAltScreen())
	
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v\n", err)
		os.Exit(1)
	}
}
