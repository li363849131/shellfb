(function () {
    "use strict";

    var terminals = {};
    var activeTermId = null;
    var tabIndex = 0;

    function createTerminal(sessionId) {
        var id = "term-" + (tabIndex++);
        var term = new Terminal({
            cursorBlink: true,
            fontSize: 14,
            fontFamily: "'JetBrains Mono', 'Fira Code', monospace",
            theme: {
                background: "#1e1e2e",
                foreground: "#cdd6f4",
                cursor: "#f5e0dc",
            },
        });

        var fitAddon = new FitAddon.FitAddon();
        var webLinksAddon = new WebLinksAddon.WebLinksAddon();
        term.loadAddon(fitAddon);
        term.loadAddon(webLinksAddon);

        var entry = {
            term: term,
            fitAddon: fitAddon,
            ws: null,
            sessionId: sessionId || null,
            closing: false,   // set by closeTab: suppresses reconnect
            retry: 0,
            retryTimer: null,
            gone: false,      // server said the session no longer exists
        };
        terminals[id] = entry;

        // Input goes through the *current* socket. Reconnecting replaces
        // entry.ws, so these handlers must not capture a socket in a closure.
        term.onData(function (data) {
            if (activeTermId !== id) return;
            var ws = entry.ws;
            if (ws && ws.readyState === WebSocket.OPEN) ws.send(data);
        });

        term.onResize(function (size) {
            var ws = entry.ws;
            if (ws && ws.readyState === WebSocket.OPEN) {
                ws.send(JSON.stringify({ type: "resize", cols: size.cols, rows: size.rows }));
            }
        });

        connect(id);
        addTab(id);
        switchTab(id);

        return id;
    }

    // Open (or reopen) the WebSocket for a terminal. An idle socket killed by a
    // proxy used to leave the page looking fine while silently eating every
    // keystroke; now the socket is rebuilt automatically and the PTY history
    // replay from the server restores the screen.
    function connect(id) {
        var entry = terminals[id];
        if (!entry || entry.closing || entry.gone) return;

        var protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
        var wsUrl = protocol + "//" + window.location.host + "/ws?token=" +
            encodeURIComponent(SessionManager.getToken());
        if (entry.sessionId) wsUrl += "&session=" + encodeURIComponent(entry.sessionId);

        var ws;
        try {
            ws = new WebSocket(wsUrl);
        } catch (e) {
            scheduleReconnect(id);
            return;
        }
        ws.binaryType = "arraybuffer";
        entry.ws = ws;

        ws.onopen = function () {
            entry.retry = 0;
            // A reconnect replays history, so clear first to avoid a duplicate
            // copy of the screen stacking under the old one.
            if (entry.everOpened) entry.term.reset();
            entry.everOpened = true;
            ws.send(JSON.stringify({ type: "resize", cols: entry.term.cols, rows: entry.term.rows }));
            if (activeTermId === id) setStatus("Connected");
        };

        ws.onmessage = function (event) {
            if (event.data instanceof ArrayBuffer) {
                entry.term.write(new Uint8Array(event.data));
                return;
            }
            try {
                var msg = JSON.parse(event.data);
                if (msg.type === "session") {
                    entry.sessionId = msg.id;
                    SessionManager.setActive(msg.id);
                    updateSessionInfo();
                } else if (msg.type === "error" && msg.code === "session_gone") {
                    // Destroyed elsewhere. Don't fight it: drop the tab.
                    entry.gone = true;
                    entry.term.write("\r\n\x1b[33m[会话已在其他位置被关闭]\x1b[0m\r\n");
                    closeTab(id, { destroyRemote: false });
                } else if (msg.type === "error") {
                    entry.term.write("\r\n\x1b[31m[" + (msg.message || "error") + "]\x1b[0m\r\n");
                }
            } catch (e) {
                entry.term.write(event.data);
            }
        };

        ws.onclose = function () {
            if (entry.ws === ws) entry.ws = null;
            if (entry.closing || entry.gone) return;
            scheduleReconnect(id);
        };

        ws.onerror = function () { /* onclose always follows; handle it there */ };
    }

    function scheduleReconnect(id) {
        var entry = terminals[id];
        if (!entry || entry.closing || entry.gone) return;
        if (entry.retryTimer) return;

        entry.retry++;
        var delay = Math.min(10000, 500 * Math.pow(2, Math.min(entry.retry - 1, 5)));
        if (activeTermId === id) {
            setStatus("Reconnecting… (" + entry.retry + ")");
        }
        entry.retryTimer = setTimeout(function () {
            entry.retryTimer = null;
            connect(id);
        }, delay);
    }

    function addTab(id) {
        var tabs = document.getElementById("tabs");
        var tab = document.createElement("div");
        tab.className = "tab";
        tab.dataset.id = id;

        var label = document.createElement("span");
        label.className = "tab-label";
        label.textContent = "Session " + (Object.keys(terminals).length);
        tab.appendChild(label);

        var closeBtn = document.createElement("span");
        closeBtn.className = "tab-close";
        closeBtn.textContent = "×";
        closeBtn.title = "关闭并销毁该会话";
        closeBtn.onclick = function (e) {
            e.stopPropagation();
            closeTab(id);
        };

        tab.appendChild(closeBtn);
        tab.onclick = function () { switchTab(id); };
        tabs.appendChild(tab);
    }

    function switchTab(id) {
        for (var tid in terminals) {
            if (terminals[tid] && terminals[tid].term) {
                if (tid !== id) {
                    if (terminals[tid].term.element) {
                        terminals[tid].term.element.style.display = "none";
                    }
                    try { terminals[tid].term.blur(); } catch(e){}
                }
            }
        }
        activeTermId = id;
        var t = terminals[id];
        var container = document.getElementById("terminal");

        if (!t.term.element) {
            t.term.open(container);
        } else {
            t.term.element.style.display = "";
            container.appendChild(t.term.element);
        }
        t.fitAddon.fit();
        t.term.focus();

        document.querySelectorAll(".tab").forEach(function (el) {
            el.classList.toggle("active", el.dataset.id === id);
        });

        if (t.sessionId) SessionManager.setActive(t.sessionId);
        updateSessionInfo();
    }

    function closeTab(id, opts) {
        var destroyRemote = !opts || opts.destroyRemote !== false;
        var t = terminals[id];
        if (t) {
            t.closing = true;
            if (t.retryTimer) { clearTimeout(t.retryTimer); t.retryTimer = null; }
            // Closing a tab means killing the session, not just this view.
            // Without the DELETE the PTY stayed alive and every other browser
            // kept listing it.
            if (destroyRemote && t.sessionId) {
                try { SessionManager.destroySession(t.sessionId); } catch (e) {}
            }
            if (t.ws) { try { t.ws.close(); } catch (e) {} }
            try { t.term.dispose(); } catch (e) {}
            delete terminals[id];
        }
        var tabEl = document.querySelector('.tab[data-id="' + id + '"]');
        if (tabEl) tabEl.remove();

        var remaining = Object.keys(terminals);
        if (remaining.length > 0) {
            if (activeTermId === id) switchTab(remaining[remaining.length - 1]);
        } else {
            activeTermId = null;
        }
        renumberTabs();
        updateSessionInfo();
    }

    // Tab labels are positional, so they have to be recomputed after a close;
    // otherwise closing "Session 1" leaves a lone tab still labelled "Session 2".
    function renumberTabs() {
        var i = 1;
        document.querySelectorAll(".tab").forEach(function (el) {
            var label = el.querySelector(".tab-label");
            if (label) label.textContent = "Session " + (i++);
        });
    }

    // Sessions can be created or destroyed from another browser. Poll the
    // server and reconcile: drop tabs whose session is gone, add tabs for
    // sessions we don't have yet.
    async function syncSessions() {
        var remote;
        try {
            remote = await SessionManager.listSessions();
        } catch (e) {
            return;
        }
        if (!remote) return;

        var remoteIds = {};
        for (var i = 0; i < remote.length; i++) remoteIds[remote[i].ID] = true;

        for (var tid in terminals) {
            var t = terminals[tid];
            if (!t || t.closing || !t.sessionId) continue;
            if (!remoteIds[t.sessionId]) {
                t.gone = true;
                closeTab(tid, { destroyRemote: false });
            }
        }

        var localIds = {};
        for (var tid2 in terminals) {
            if (terminals[tid2] && terminals[tid2].sessionId) {
                localIds[terminals[tid2].sessionId] = true;
            }
        }
        for (var j = 0; j < remote.length; j++) {
            if (!localIds[remote[j].ID]) createTerminal(remote[j].ID);
        }
    }

    function setStatus(text) {
        document.getElementById("connection-status").textContent = text;
    }

    function updateSessionInfo() {
        var info = document.getElementById("session-info");
        var count = Object.keys(terminals).length;
        info.textContent = count + " session" + (count !== 1 ? "s" : "");
    }

    function init() {
        var token = SessionManager.getToken();
        if (!token) {
            document.getElementById("login-screen").style.display = "flex";
        } else {
            startApp();
        }

        document.getElementById("login-form").onsubmit = async function (e) {
            e.preventDefault();
            var user = document.getElementById("username").value;
            var pass = document.getElementById("password").value;
            try {
                await SessionManager.login(user, pass);
                document.getElementById("login-screen").style.display = "none";
                startApp();
            } catch (err) {
                document.getElementById("login-error").textContent = err.message;
            }
        };
    }

    async function startApp() {
        document.getElementById("app").style.display = "flex";
        
        try {
            var existingSessions = await SessionManager.listSessions();
            if (existingSessions && existingSessions.length > 0) {
                for (var i = 0; i < existingSessions.length; i++) {
                    createTerminal(existingSessions[i].ID);
                }
            } else {
                createTerminal();
            }
        } catch (e) {
            createTerminal();
        }

        document.getElementById("new-tab").onclick = function () {
            createTerminal();
        };

        var fileInput = document.getElementById("file-upload-input");
        var uploadBtn = document.getElementById("upload-btn");
        if (uploadBtn && fileInput) {
            uploadBtn.onclick = function () {
                fileInput.click();
            };
            fileInput.onchange = function () {
                if (fileInput.files && fileInput.files.length > 0) {
                    for (var i = 0; i < fileInput.files.length; i++) {
                        uploadFileAndInsertPath(fileInput.files[i]);
                    }
                    fileInput.value = "";
                }
            };
        }

        setupDragAndDropAndPaste();
        setupPreviewPanel();
        setupHelpModal();
        setupExternalButtons();
        setupVoiceInput();
        setupAIInput();

        // Reconcile with other browsers every few seconds, and immediately when
        // this tab regains focus (the common case: you deleted a session
        // elsewhere and switched back).
        setInterval(syncSessions, 5000);
        document.addEventListener("visibilitychange", function () {
            if (!document.hidden) syncSessions();
        });

        window.addEventListener("resize", function () {
            if (activeTermId && terminals[activeTermId]) {
                terminals[activeTermId].fitAddon.fit();
            }
        });
    }

    // ---------------- AI: natural language -> shell command ----------------
    //
    // The translated command is only *typed* into the terminal. The user presses
    // Enter. That is deliberate: a model can misread an instruction, and an
    // auto-executed `rm` cannot be taken back.

    var aiAvailable = false;   // server has credentials configured

    function aiEnabled() {
        var cb = document.getElementById("voice-ai-mode");
        return aiAvailable && !!(cb && cb.checked);
    }

    function looksDangerous(cmd, explain) {
        if (explain && explain.indexOf("危险") === 0) return true;
        return /(^|[\s;&|])(rm|dd|mkfs|shred|fdisk|parted)\s|--no-preserve-root|chmod\s+-R|chown\s+-R|kill\s+-9|>\s*\/(etc|boot|dev|sys)\b|\bmv\b[^|;]*\s\/(etc|boot|dev|sys)\b/.test(cmd);
    }

    // Type text into the terminal without submitting it.
    function typeIntoTerminal(text) {
        if (!activeTermId || !terminals[activeTermId]) {
            alert("请先连接或选中终端标签页");
            return false;
        }
        var ws = terminals[activeTermId].ws;
        if (!ws || ws.readyState !== WebSocket.OPEN) {
            alert("终端未连接，请稍候重试");
            return false;
        }
        ws.send(text);
        focusActiveTerm();
        return true;
    }

    function focusActiveTerm() {
        if (activeTermId && terminals[activeTermId] && terminals[activeTermId].term) {
            try { terminals[activeTermId].term.focus(); } catch (e) {}
        }
    }

    async function aiTranslate(text) {
        var resp = await fetch("/api/ai/translate?token=" +
            encodeURIComponent(SessionManager.getToken()), {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ text: text, os: navigator.platform || "" }),
        });
        var data = await resp.json();
        if (data.error) throw new Error(data.error);
        return data;
    }

    // Used by the voice panel when AI mode is on.
    function translateAndSend(text) {
        aiTranslate(text).then(function (data) {
            if (!data.command) {
                alert("AI 未能转成命令：" + (data.explain || "请说得更具体一些"));
                return;
            }
            showAIBar();
            renderAIResult(data);
        }).catch(function (err) {
            alert("AI 转换失败: " + err.message);
        });
    }

    function showAIBar() {
        var bar = document.getElementById("ai-bar");
        if (!bar) return;
        bar.style.display = "flex";
        var input = document.getElementById("ai-bar-input");
        if (input) { input.focus(); input.select(); }
    }

    function hideAIBar() {
        var bar = document.getElementById("ai-bar");
        if (bar) bar.style.display = "none";
        focusActiveTerm();
    }

    function renderAIResult(data) {
        var box = document.getElementById("ai-bar-result");
        var cmdEl = document.getElementById("ai-bar-command");
        var expEl = document.getElementById("ai-bar-explain");
        if (!box || !cmdEl) return;

        cmdEl.textContent = data.command;
        var danger = looksDangerous(data.command, data.explain);
        cmdEl.style.color = danger ? "#f38ba8" : "#a6e3a1";
        cmdEl.style.borderColor = danger ? "#f38ba8" : "#313244";
        if (expEl) {
            expEl.textContent = (danger ? "⚠️ " : "") + (data.explain || "");
            expEl.style.color = danger ? "#f38ba8" : "#a6adc8";
        }
        box.style.display = "flex";
        box.dataset.command = data.command;
    }

    function setupAIInput() {
        var btn = document.getElementById("ai-btn");
        var bar = document.getElementById("ai-bar");
        var input = document.getElementById("ai-bar-input");
        var closeBtn = document.getElementById("ai-bar-close");
        var insertBtn = document.getElementById("ai-bar-insert");
        var statusEl = document.getElementById("ai-bar-status");
        var resultBox = document.getElementById("ai-bar-result");
        var aiRow = document.getElementById("voice-ai-row");
        if (!bar || !input) return;

        // Hide the controls entirely when the server has no API key, rather than
        // offering a button that always errors.
        fetch("/api/ai/status?token=" + encodeURIComponent(SessionManager.getToken()))
            .then(function (r) { return r.json(); })
            .then(function (data) {
                aiAvailable = !!(data && data.enabled);
                if (aiAvailable) {
                    if (btn) btn.style.display = "";
                    if (aiRow) aiRow.style.display = "flex";
                }
            })
            .catch(function () { /* leave AI hidden */ });

        function setBusy(busy, msg) {
            if (statusEl) statusEl.textContent = msg || "";
            input.disabled = busy;
        }

        function run() {
            var text = input.value.trim();
            if (!text) return;
            if (resultBox) resultBox.style.display = "none";
            setBusy(true, "转换中…");
            aiTranslate(text).then(function (data) {
                setBusy(false, "");
                if (!data.command) {
                    if (statusEl) statusEl.textContent = data.explain || "无法转成命令";
                    return;
                }
                renderAIResult(data);
            }).catch(function (err) {
                setBusy(false, err.message);
            });
        }

        input.addEventListener("keydown", function (e) {
            if (e.key === "Enter") {
                e.preventDefault();
                // Enter in the *result* state inserts; otherwise it translates.
                if (resultBox && resultBox.style.display === "flex" && resultBox.dataset.command) {
                    insert();
                } else {
                    run();
                }
            } else if (e.key === "Escape") {
                e.stopPropagation();
                hideAIBar();
            } else if (resultBox) {
                // Editing the request invalidates the previous answer.
                resultBox.style.display = "none";
                resultBox.dataset.command = "";
            }
        });

        function insert() {
            var cmd = resultBox ? resultBox.dataset.command : "";
            if (!cmd) return;
            if (typeIntoTerminal(cmd)) {
                hideAIBar();
                input.value = "";
                if (resultBox) { resultBox.style.display = "none"; resultBox.dataset.command = ""; }
            }
        }

        if (insertBtn) insertBtn.addEventListener("click", insert);
        if (closeBtn) closeBtn.addEventListener("click", hideAIBar);
        if (btn) btn.addEventListener("click", function () {
            if (bar.style.display === "flex") { hideAIBar(); return; }
            showAIBar();
        });

        document.addEventListener("keydown", function (e) {
            if (e.ctrlKey && e.shiftKey && (e.key === "A" || e.key === "a")) {
                e.preventDefault();
                if (!aiAvailable) return;
                if (bar.style.display === "flex") hideAIBar(); else showAIBar();
            }
        });
    }

    function setupExternalButtons() {
        var fbBtn = document.getElementById("filebrowser-btn");
        var vsBtn = document.getElementById("vscode-btn");

        function handleServiceClick(serviceType) {
            var host  = window.location.hostname;
            var proto = window.location.protocol;
            var isDirectPort = window.location.port === "30009" || /^\d+\.\d+\.\d+\.\d+$/.test(host);

            // 1. 匹配用户自建 Cloudflare 域名 (youngteach.com)
            if (host.indexOf("youngteach.com") !== -1) {
                if (serviceType === "filebrowser") {
                    window.open(proto + "//pan.youngteach.com", "_blank");
                    return;
                } else if (serviceType === "vscode") {
                    window.open(proto + "//code.youngteach.com", "_blank");
                    return;
                }
            }

            // 2. IP 直连或端口直连
            if (isDirectPort) {
                var port = serviceType === "filebrowser" ? "30010" : "30011";
                window.open(proto + "//" + host + ":" + port, "_blank");
                return;
            }

            // 3. 通用 trycloudflare 临时域名回退
            var token = SessionManager.getToken();
            fetch("/api/links?token=" + encodeURIComponent(token || ""))
                .then(function(r) { if (!r.ok) throw new Error("HTTP " + r.status); return r.json(); })
                .then(function(data) {
                    var targetUrl = serviceType === "filebrowser" ? data.filebrowser_url : data.vscode_url;
                    if (!targetUrl) {
                        var fallbackPort = serviceType === "filebrowser" ? "30010" : "30011";
                        targetUrl = proto + "//" + host + ":" + fallbackPort;
                    }
                    window.open(targetUrl, "_blank");
                })
                .catch(function() {
                    var fallbackPort = serviceType === "filebrowser" ? "30010" : "30011";
                    window.open(proto + "//" + host + ":" + fallbackPort, "_blank");
                });
        }

        if (fbBtn) {
            fbBtn.onclick = function() { handleServiceClick("filebrowser"); };
        }
        if (vsBtn) {
            vsBtn.onclick = function() { handleServiceClick("vscode"); };
        }
    }

    function setupHelpModal() {
        var helpBtn   = document.getElementById("help-btn");
        var modal     = document.getElementById("help-modal-overlay");
        var closeBtn  = document.getElementById("help-modal-close");
        var okBtn     = document.getElementById("help-modal-ok");

        function openModal(e) {
            if (e) e.stopPropagation();
            if (modal) modal.style.display = "flex";
        }
        function closeModal(e) {
            if (e) e.stopPropagation();
            if (modal) modal.style.display = "none";
        }

        if (helpBtn) {
            helpBtn.onclick = openModal;
            helpBtn.addEventListener("click", openModal);
        }
        if (closeBtn) closeBtn.onclick = closeModal;
        if (okBtn) okBtn.onclick = closeModal;
        if (modal) {
            modal.onclick = function(e) {
                if (e.target === modal) closeModal(e);
            };
        }
        window.addEventListener("keydown", function(e) {
            if (e.key === "Escape" && modal && modal.style.display === "flex") {
                closeModal(e);
            }
        });
    }

    var IMAGE_EXTS = /\.(png|jpe?g|gif|webp|bmp|svg|ico|avif|tiff?)$/i;

    var IMAGE_EXTS_SET  = {"png":1,"jpg":1,"jpeg":1,"gif":1,"webp":1,"bmp":1,"svg":1,"ico":1,"avif":1,"tiff":1,"tif":1};
    var PDF_EXTS_SET    = {"pdf":1};
    var OFFICE_EXTS_SET = {"doc":1,"docx":1,"xls":1,"xlsx":1,"ppt":1,"pptx":1,"odt":1,"ods":1,"odp":1,"rtf":1};
    var TEXT_EXTS_SET   = {"txt":1,"md":1,"markdown":1,"json":1,"yaml":1,"yml":1,"toml":1,"csv":1,"log":1,
        "conf":1,"cfg":1,"ini":1,"env":1,"js":1,"ts":1,"jsx":1,"tsx":1,"py":1,"go":1,"sh":1,"bash":1,
        "zsh":1,"fish":1,"html":1,"htm":1,"css":1,"xml":1,"sql":1,"java":1,"c":1,"cpp":1,"h":1,"hpp":1,
        "rs":1,"rb":1,"php":1,"r":1,"kt":1,"swift":1,"dart":1,"vue":1,"lua":1,"vim":1,"el":1,"hs":1,
        "ml":1,"scala":1,"tex":1,"diff":1,"patch":1,"tf":1,"hcl":1,"proto":1,"graphql":1,"gql":1};

    var FILE_ICONS = {
        png:"🖼️",jpg:"🖼️",jpeg:"🖼️",gif:"🖼️",webp:"🖼️",bmp:"🖼️",svg:"🖼️",ico:"🖼️",avif:"🖼️",tiff:"🖼️",tif:"🖼️",
        pdf:"📕",
        doc:"📝",docx:"📝",odt:"📝",
        xls:"📊",xlsx:"📊",ods:"📊",csv:"📊",
        ppt:"📽️",pptx:"📽️",odp:"📽️",
        zip:"📦",tar:"📦",gz:"📦",bz2:"📦","7z":"📦",rar:"📦",zst:"📦",
        mp3:"🎵",wav:"🎵",flac:"🎵",ogg:"🎵",m4a:"🎵",aac:"🎵",
        mp4:"🎬",avi:"🎬",mkv:"🎬",mov:"🎬",webm:"🎬",
        py:"🐍",go:"🐹",js:"⚡",ts:"⚡",jsx:"⚡",tsx:"⚡",
        sh:"🔧",bash:"🔧",zsh:"🔧",fish:"🔧",
        html:"🌐",htm:"🌐",css:"🎨",
        json:"📋",yaml:"📋",yml:"📋",toml:"📋",xml:"📋",
        md:"📖",txt:"📄",log:"📄",
        java:"☕",c:"🔵",cpp:"🔵",rs:"🦀",rb:"💎",php:"🐘",
        sql:"🗃️",diff:"📑",patch:"📑",
    };

    var fileBrowserTimer = null;
    var currentPath = "";

    function getExt(filename) {
        var i = filename.lastIndexOf(".");
        return i > 0 ? filename.slice(i + 1).toLowerCase() : "";
    }

    function getFileIcon(item) {
        if (typeof item === "object" && (item.is_dir || item.isDir || item.IsDir)) return "📁";
        var name = typeof item === "string" ? item : (item ? item.name || "" : "");
        if (name.startsWith(".")) return "⚙️";
        return FILE_ICONS[getExt(name)] || "📎";
    }

    function formatSize(bytes) {
        if (bytes < 1024) return bytes + " B";
        if (bytes < 1048576) return (bytes / 1024).toFixed(1) + " KB";
        return (bytes / 1048576).toFixed(1) + " MB";
    }

    function setupPreviewPanel() {
        var panel       = document.getElementById("preview-panel");
        var handle      = document.getElementById("panel-resize-handle");
        var toggleBtn   = document.getElementById("preview-toggle-btn");
        var closeBtn    = document.getElementById("preview-close-btn");
        var refreshBtn  = document.getElementById("preview-refresh-btn");

        function openPanel() {
            if (!panel) return;
            panel.style.display  = "flex";
            if (handle) handle.style.display = "block";
            if (toggleBtn) toggleBtn.style.background = "#45475a";
            loadFileList(currentPath);
            startFileBrowserRefresh();
            if (activeTermId && terminals[activeTermId]) terminals[activeTermId].fitAddon.fit();
        }
        function closePanel() {
            if (!panel) return;
            panel.style.display = "none";
            if (handle) handle.style.display = "none";
            if (toggleBtn) toggleBtn.style.background = "#313244";
            stopFileBrowserRefresh();
            if (activeTermId && terminals[activeTermId]) terminals[activeTermId].fitAddon.fit();
        }

        if (toggleBtn) toggleBtn.onclick = function() {
            panel && panel.style.display !== "none" ? closePanel() : openPanel();
        };
        if (closeBtn)   closeBtn.onclick   = closePanel;
        if (refreshBtn) refreshBtn.onclick = function() { loadFileList(currentPath); };

        setupPanelResize();
        setupContentResize();
    }

    function startFileBrowserRefresh() {
        stopFileBrowserRefresh();
        fileBrowserTimer = setInterval(function() {
            var panel = document.getElementById("preview-panel");
            if (panel && panel.style.display !== "none") loadFileList();
        }, 5000);
    }

    function stopFileBrowserRefresh() {
        if (fileBrowserTimer) { clearInterval(fileBrowserTimer); fileBrowserTimer = null; }
    }

    function setupPanelResize() {
        var handle = document.getElementById("panel-resize-handle");
        var panel  = document.getElementById("preview-panel");
        if (!handle || !panel) return;
        var dragging = false, startX, startW;

        handle.addEventListener("mousedown", function(e) {
            dragging = true; startX = e.clientX; startW = panel.offsetWidth;
            document.body.style.cursor = "col-resize";
            document.body.style.userSelect = "none";
            e.preventDefault();
        });
        handle.addEventListener("mouseover",  function() { handle.style.background = "#89b4fa"; });
        handle.addEventListener("mouseout",   function() { if (!dragging) handle.style.background = "#1e1e2e"; });
        document.addEventListener("mousemove", function(e) {
            if (!dragging) return;
            var newW = Math.max(200, Math.min(window.innerWidth * 0.75, startW + startX - e.clientX));
            panel.style.width = newW + "px";
            if (activeTermId && terminals[activeTermId]) terminals[activeTermId].fitAddon.fit();
        });
        document.addEventListener("mouseup", function() {
            if (dragging) { dragging = false; handle.style.background = "#1e1e2e"; document.body.style.cursor = ""; document.body.style.userSelect = ""; }
        });
    }

    function setupContentResize() {
        var handle  = document.getElementById("content-resize-handle");
        var section = document.getElementById("file-browser-section");
        if (!handle || !section) return;
        var dragging = false, startY, startH;

        handle.addEventListener("mousedown", function(e) {
            dragging = true; startY = e.clientY; startH = section.offsetHeight;
            document.body.style.cursor = "row-resize";
            document.body.style.userSelect = "none";
            e.preventDefault();
        });
        handle.addEventListener("mouseover",  function() { handle.style.background = "#89b4fa"; });
        handle.addEventListener("mouseout",   function() { if (!dragging) handle.style.background = "#313244"; });
        document.addEventListener("mousemove", function(e) {
            if (!dragging) return;
            var newH = Math.max(60, Math.min(window.innerHeight * 0.7, startH + e.clientY - startY));
            section.style.height = newH + "px";
        });
        document.addEventListener("mouseup", function() {
            if (dragging) { dragging = false; handle.style.background = "#313244"; document.body.style.cursor = ""; document.body.style.userSelect = ""; }
        });
    }

    function loadFileList(dirPath) {
        if (typeof dirPath === "string") {
            currentPath = dirPath;
        }
        var token = SessionManager.getToken();
        if (!token) return;
        var url = "/api/files?token=" + encodeURIComponent(token);
        if (currentPath) {
            url += "&dir=" + encodeURIComponent(currentPath);
        }
        fetch(url)
            .then(function(r) { if (!r.ok) throw new Error(r.status); return r.json(); })
            .then(function(files) { renderFileList(files, token); })
            .catch(function() {
                var list = document.getElementById("file-list");
                if (list) list.innerHTML = "<div style='color:#f38ba8;padding:10px 12px;font-size:12px;'>⚠ 加载失败</div>";
            });
    }

    function renderFileList(files, token) {
        var list  = document.getElementById("file-list");
        var badge = document.getElementById("file-count-badge");
        var titleEl = document.getElementById("file-browser-title");
        if (titleEl) {
            titleEl.textContent = "📁 /root" + (currentPath ? ("/" + currentPath) : "");
        }
        if (!list) return;

        if (badge) badge.textContent = files.length ? "(" + files.length + ")" : "";

        // Remember selected item
        var selectedName = "";
        var oldSel = list.querySelector(".fb-selected");
        if (oldSel) selectedName = oldSel.dataset.name || "";

        list.innerHTML = "";

        // If inside a subfolder, render '..' row to navigate up
        if (currentPath) {
            var upRow = document.createElement("div");
            upRow.style.cssText = "display:flex;align-items:center;padding:5px 8px;border-radius:5px;cursor:pointer;gap:8px;transition:background 0.1s;";
            var upIcon = document.createElement("span");
            upIcon.textContent = "📁";
            upIcon.style.cssText = "font-size:15px;flex-shrink:0;width:20px;text-align:center;";
            var upInfo = document.createElement("div");
            upInfo.style.cssText = "flex:1;min-width:0;";
            var upName = document.createElement("div");
            upName.textContent = ".. (返回上级)";
            upName.style.cssText = "color:#a6e3a1;font-size:12px;font-weight:bold;font-family:'JetBrains Mono',monospace;";
            upInfo.appendChild(upName);
            upRow.appendChild(upIcon);
            upRow.appendChild(upInfo);
            upRow.addEventListener("mouseover", function() { upRow.style.background = "#313244"; });
            upRow.addEventListener("mouseout",  function() { upRow.style.background = ""; });
            upRow.addEventListener("click", function() {
                var parts = currentPath.split("/").filter(Boolean);
                parts.pop();
                loadFileList(parts.join("/"));
            });
            list.appendChild(upRow);
        }

        if (!files || files.length === 0) {
            var empty = document.createElement("div");
            empty.style.cssText = "color:#585b70;padding:16px;text-align:center;font-size:12px;";
            empty.innerHTML = "📂 " + (currentPath ? currentPath : "root") + " 目录为空";
            list.appendChild(empty);
            return;
        }

        files.forEach(function(f) {
            var fullRelPath = currentPath ? (currentPath + "/" + f.name) : f.name;
            var isDir = !!(f.is_dir || f.isDir || f.IsDir);
            var row = document.createElement("div");
            row.style.cssText = "display:flex;align-items:center;padding:5px 8px;border-radius:5px;cursor:pointer;gap:8px;transition:background 0.1s;";
            row.dataset.name = f.name;
            if (f.name === selectedName) { row.style.background = "#45475a"; row.classList.add("fb-selected"); }

            var icon = document.createElement("span");
            icon.textContent = getFileIcon(f);
            icon.style.cssText = "font-size:15px;flex-shrink:0;width:20px;text-align:center;";

            var info = document.createElement("div");
            info.style.cssText = "flex:1;min-width:0;";

            var name = document.createElement("div");
            name.textContent = f.name;
            name.style.cssText = "color:#cdd6f4;font-size:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-family:'JetBrains Mono',monospace;";
            if (f.name.startsWith(".")) {
                name.style.color = "#89dceb";
            }

            var meta = document.createElement("div");
            meta.textContent = isDir ? "文件夹" : (formatSize(f.size) + (f.modified ? " · " + f.modified : ""));
            meta.style.cssText = "color:#6c7086;font-size:10px;margin-top:1px;";

            info.appendChild(name);
            info.appendChild(meta);

            var delBtn = document.createElement("button");
            delBtn.textContent = "🗑️";
            delBtn.title = "删除";
            delBtn.style.cssText = "display:none;color:#f38ba8;background:none;border:none;padding:2px 4px;cursor:pointer;font-size:12px;border-radius:4px;flex-shrink:0;";
            delBtn.addEventListener("mouseover", function() { delBtn.style.background = "#f38ba833"; });
            delBtn.addEventListener("mouseout",  function() { delBtn.style.background = "none"; });

            row.addEventListener("mouseover", function() {
                if (!row.classList.contains("fb-selected")) row.style.background = "#313244";
                delBtn.style.display = "inline-block";
            });
            row.addEventListener("mouseout",  function() {
                if (!row.classList.contains("fb-selected")) row.style.background = "";
                delBtn.style.display = "none";
            });

            delBtn.addEventListener("click", function(e) {
                e.stopPropagation();
                var fnSpan = document.getElementById("preview-content-filename");
                var isOpen = fnSpan && fnSpan.textContent && fnSpan.textContent.indexOf(f.name) === 0;

                if (isOpen) {
                    if (!confirm("当前文件 \"" + f.name + "\" 正在打开预览中，确定要删除吗？")) return;
                }

                fetch("/api/delete?file=" + encodeURIComponent(fullRelPath) + "&token=" + encodeURIComponent(token), { method: "POST" })
                    .then(function(r) { if (!r.ok) throw new Error("HTTP " + r.status); return r.json(); })
                    .then(function() {
                        if (isOpen) {
                            var content = document.getElementById("preview-content");
                            var header = document.getElementById("preview-content-header");
                            var editBtn = document.getElementById("preview-edit-btn");
                            if (content) content.innerHTML = "<div style='color:#585b70;font-size:12px;padding:16px;text-align:center;'>请从文件列表中选择文件预览</div>";
                            if (header) header.style.display = "none";
                            if (editBtn) editBtn.style.display = "none";
                        }
                        loadFileList(currentPath);
                    })
                    .catch(function(err) {
                        alert("删除失败: " + err.message);
                    });
            });

            row.appendChild(icon);
            row.appendChild(info);
            row.appendChild(delBtn);

            row.addEventListener("click", function() {
                if (isDir) {
                    loadFileList(fullRelPath);
                } else {
                    list.querySelectorAll(".fb-selected").forEach(function(el) {
                        el.classList.remove("fb-selected"); el.style.background = "";
                        var b = el.querySelector("button");
                        if (b) b.style.display = "none";
                    });
                    row.classList.add("fb-selected"); row.style.background = "#45475a";
                    delBtn.style.display = "inline-block";
                    previewFile(fullRelPath, token);
                }
            });
            list.appendChild(row);
        });
    }

    function renderTextEditor(text, filename, url, content, fnSpan) {
        content.style.cssText = "flex:1;overflow:hidden;display:flex;flex-direction:column;min-height:0;position:relative;";
        content.innerHTML = "";

        var editBtn = document.getElementById("preview-edit-btn");
        if (editBtn) {
            editBtn.style.display = "inline-block";
            editBtn.textContent = "✏️ 编辑";
            editBtn.style.color = "#a6e3a1";
            editBtn.disabled = false;
        }

        var isEditing = false;

        var container = document.createElement("div");
        container.style.cssText = "flex:1;display:flex;overflow:hidden;min-height:0;background:#11111b;";

        // Line numbers column (exact integer 20px line-height)
        var lineCol = document.createElement("div");
        lineCol.style.cssText = "padding:12px 10px 12px 12px;color:#585b70;font-family:'JetBrains Mono','Fira Code',Consolas,monospace;font-size:13px;line-height:20px;text-align:right;user-select:none;border-right:1px solid #313244;background:#181825;overflow:hidden;flex-shrink:0;box-sizing:border-box;white-space:pre;";

        // Unified Textarea for both View and Edit (exact integer 20px line-height & padding matching lineCol)
        var textarea = document.createElement("textarea");
        textarea.style.cssText = "flex:1;width:100%;height:100%;margin:0;padding:12px 16px;color:#cdd6f4;background:transparent;font-family:'JetBrains Mono','Fira Code',Consolas,monospace;font-size:13px;line-height:20px;white-space:pre;word-wrap:normal;overflow:auto;tab-size:4;border:none;outline:none;resize:none;box-sizing:border-box;";
        textarea.value = text || "";
        textarea.readOnly = true;

        container.appendChild(lineCol);
        container.appendChild(textarea);
        content.appendChild(container);

        // Autocomplete tooltip box
        var suggestBox = document.createElement("div");
        suggestBox.style.cssText = "display:none;position:absolute;z-index:9999;background:#1e1e2e;border:1px solid #585b70;border-radius:6px;box-shadow:0 8px 24px rgba(0,0,0,0.6);max-height:160px;overflow-y:auto;font-family:'JetBrains Mono',monospace;font-size:12px;";
        content.appendChild(suggestBox);

        function updateLineNumbers(txt) {
            var lines = (txt || "").split("\n").length;
            var numStr = "";
            for (var i = 1; i <= lines; i++) {
                numStr += i + "\n";
            }
            lineCol.textContent = numStr;
            if (fnSpan) fnSpan.textContent = filename + "  (" + lines + " 行)";
        }

        updateLineNumbers(text);

        // Continuous scroll synchronization (1:1 alignment)
        textarea.onscroll = function() {
            lineCol.scrollTop = textarea.scrollTop;
            suggestBox.style.display = "none";
        };

        // Token extraction for autocomplete
        function getDocumentTokens(val) {
            var words = val.match(/[a-zA-Z_]\w{2,}/g) || [];
            var set = {};
            words.forEach(function(w) { set[w] = true; });
            return Object.keys(set);
        }

        var activeSuggestIndex = 0;
        var currentSuggestions = [];

        function showSuggestions(prefix) {
            if (!prefix || prefix.length < 2) {
                suggestBox.style.display = "none";
                return;
            }
            var tokens = getDocumentTokens(textarea.value);
            currentSuggestions = tokens.filter(function(t) {
                return t.toLowerCase().startsWith(prefix.toLowerCase()) && t !== prefix;
            }).slice(0, 6);

            if (currentSuggestions.length === 0) {
                suggestBox.style.display = "none";
                return;
            }

            suggestBox.innerHTML = "";
            activeSuggestIndex = 0;

            currentSuggestions.forEach(function(s, idx) {
                var item = document.createElement("div");
                item.style.cssText = "padding:4px 10px;cursor:pointer;color:" + (idx === 0 ? "#89b4fa;background:#313244;" : "#cdd6f4;");
                item.innerHTML = "<span style='color:#cba6f7;margin-right:6px;'>✦</span>" + s;
                item.onmousedown = function(e) {
                    e.preventDefault();
                    applySuggestion(s, prefix);
                };
                suggestBox.appendChild(item);
            });

            var pos = textarea.selectionStart;
            var valBefore = textarea.value.slice(0, pos);
            var lineCount = valBefore.split("\n").length;
            var topPos = Math.max(10, lineCount * 20 - textarea.scrollTop + 12);
            suggestBox.style.top = (topPos + 20) + "px";
            suggestBox.style.left = "60px";
            suggestBox.style.display = "block";
        }

        function applySuggestion(word, prefix) {
            var pos = textarea.selectionStart;
            var before = textarea.value.slice(0, pos - prefix.length);
            var after = textarea.value.slice(pos);
            textarea.value = before + word + after;
            textarea.selectionStart = textarea.selectionEnd = before.length + word.length;
            updateLineNumbers(textarea.value);
            suggestBox.style.display = "none";
        }

        textarea.oninput = function() {
            updateLineNumbers(textarea.value);
            if (isEditing) {
                var pos = textarea.selectionStart;
                var val = textarea.value.slice(0, pos);
                var match = val.match(/[a-zA-Z_]\w*$/);
                if (match) {
                    showSuggestions(match[0]);
                } else {
                    suggestBox.style.display = "none";
                }
            }
        };

        textarea.onkeydown = function(e) {
            if (!isEditing) return;
            if (suggestBox.style.display === "block" && (e.key === "Enter" || e.key === "Tab")) {
                if (currentSuggestions.length > 0) {
                    e.preventDefault();
                    var pos = textarea.selectionStart;
                    var val = textarea.value.slice(0, pos);
                    var match = val.match(/[a-zA-Z_]\w*$/);
                    applySuggestion(currentSuggestions[activeSuggestIndex || 0], match ? match[0] : "");
                    return;
                }
            }
            if (e.key === "Tab") {
                e.preventDefault();
                var start = textarea.selectionStart;
                var end = textarea.selectionEnd;
                textarea.value = textarea.value.substring(0, start) + "    " + textarea.value.substring(end);
                textarea.selectionStart = textarea.selectionEnd = start + 4;
                updateLineNumbers(textarea.value);
            }
            if (e.ctrlKey && (e.key === "s" || e.key === "S")) {
                e.preventDefault();
                saveCurrentText();
            }
        };

        function saveCurrentText() {
            if (!editBtn) return;
            var updatedText = textarea.value;
            editBtn.textContent = "⏳ 保存中...";
            editBtn.disabled = true;

            var formData = new FormData();
            var file = new File([updatedText], filename, { type: "text/plain" });
            formData.append("file", file);

            var token = SessionManager.getToken();
            var uploadUrl = "/api/upload?token=" + encodeURIComponent(token);

            fetch(uploadUrl, { method: "POST", body: formData })
                .then(function(r) { if (!r.ok) throw new Error("HTTP " + r.status); return r.json(); })
                .then(function() {
                    editBtn.textContent = "✏️ 编辑";
                    editBtn.disabled = false;
                    editBtn.style.color = "#a6e3a1";
                    textarea.readOnly = true;
                    textarea.style.background = "transparent";
                    isEditing = false;
                    loadFileList();
                })
                .catch(function(err) {
                    editBtn.textContent = "❌ 保存失败";
                    editBtn.disabled = false;
                    setTimeout(function() {
                        editBtn.textContent = "💾 保存";
                        editBtn.style.color = "#89b4fa";
                    }, 2000);
                });
        }

        if (editBtn) {
            editBtn.onclick = function() {
                if (!isEditing) {
                    isEditing = true;
                    textarea.readOnly = false;
                    textarea.style.background = "#181825";
                    textarea.focus();
                    editBtn.textContent = "💾 保存";
                    editBtn.style.color = "#89b4fa";
                } else {
                    saveCurrentText();
                }
            };
        }
    }

    function renderExcelViewer(ab, filename, content) {
        content.style.cssText = "flex:1;overflow:hidden;display:flex;flex-direction:column;min-height:0;background:#11111b;";
        content.innerHTML = "";

        var workbook = XLSX.read(ab, { type: "array" });
        if (!workbook || !workbook.SheetNames || workbook.SheetNames.length === 0) {
            content.innerHTML = "<div style='color:#f38ba8;padding:16px;font-size:13px;'>❌ 工作表为空或无法解析</div>";
            return;
        }

        var tableWrap = document.createElement("div");
        tableWrap.style.cssText = "flex:1;overflow:auto;min-height:0;padding:12px;";

        var tabContainer = document.createElement("div");
        tabContainer.style.cssText = "display:flex;background:#181825;border-top:1px solid #313244;overflow-x:auto;flex-shrink:0;height:32px;align-items:center;padding:0 8px;gap:4px;";

        function renderSheet(sheetName) {
            tableWrap.innerHTML = "";
            var ws = workbook.Sheets[sheetName];
            if (!ws) return;

            var html = XLSX.utils.sheet_to_html(ws, { editable: false });

            var wrapper = document.createElement("div");
            wrapper.style.cssText = "display:inline-block;min-width:100%;";
            wrapper.innerHTML = html;

            var table = wrapper.querySelector("table");
            if (table) {
                table.style.cssText = "border-collapse:collapse;font-family:'JetBrains Mono','Fira Code',monospace;font-size:12px;color:#cdd6f4;width:100%;";
                var cells = table.querySelectorAll("td, th");
                cells.forEach(function(td) {
                    td.style.border = "1px solid #313244";
                    td.style.padding = "6px 12px";
                    td.style.whiteSpace = "nowrap";
                });
                var headers = table.querySelectorAll("tr:first-child td, tr:first-child th, th");
                headers.forEach(function(th) {
                    th.style.background = "#1e1e2e";
                    th.style.color = "#89b4fa";
                    th.style.fontWeight = "600";
                    th.style.position = "sticky";
                    th.style.top = "0";
                    th.style.zIndex = "2";
                });
            }

            tableWrap.appendChild(wrapper);
        }

        workbook.SheetNames.forEach(function(sName, idx) {
            var tab = document.createElement("button");
            tab.textContent = sName;
            tab.style.cssText = "padding:2px 10px;font-size:11px;border-radius:4px;border:1px solid #45475a;cursor:pointer;background:" + (idx === 0 ? "#45475a;" : "#313244;") + "color:" + (idx === 0 ? "#89b4fa;" : "#a6adc8;");
            tab.onclick = function() {
                tabContainer.querySelectorAll("button").forEach(function(btn) {
                    btn.style.background = "#313244";
                    btn.style.color = "#a6adc8";
                });
                tab.style.background = "#45475a";
                tab.style.color = "#89b4fa";
                renderSheet(sName);
            };
            tabContainer.appendChild(tab);
        });

        content.appendChild(tableWrap);
        content.appendChild(tabContainer);

        renderSheet(workbook.SheetNames[0]);
    }

    function renderOfficeConversion(url, convertUrl, filename, ext, content) {
        content.style.cssText = "flex:1;overflow:hidden;display:flex;flex-direction:column;min-height:0;";
        content.innerHTML = "<div style='padding:16px;color:#89b4fa;font-size:13px;'>⏳ 正在转换 Office 文档... (请稍候)</div>";
        fetch(convertUrl)
            .then(function(r) { if (!r.ok) throw new Error("HTTP " + r.status); return r.blob(); })
            .then(function(blob) {
                var blobUrl = URL.createObjectURL(blob);
                content.innerHTML = "";
                content.style.cssText = "flex:1;overflow:hidden;display:flex;flex-direction:column;min-height:0;";
                var iframe = document.createElement("iframe");
                iframe.src   = blobUrl;
                iframe.title = filename;
                iframe.style.cssText = "flex:1;width:100%;border:none;background:#fff;min-height:0;";
                content.appendChild(iframe);
            })
            .catch(function(e) {
                content.style.cssText = "flex:1;overflow:auto;display:flex;align-items:center;justify-content:center;min-height:0;padding:16px;";
                var fileIcon = getFileIcon(filename);
                var extLabel = ext.toUpperCase();
                content.innerHTML =
                    "<div style='text-align:center;'>" +
                    "<div style='font-size:52px;margin-bottom:12px;'>" + fileIcon + "</div>" +
                    "<div style='color:#cdd6f4;font-size:13px;font-weight:600;margin-bottom:4px;word-break:break-all;'>" + filename + "</div>" +
                    "<div style='color:#f38ba8;font-size:12px;margin-bottom:20px;'>转换失败 (" + e.message + ")</div>" +
                    "<a href='" + url + "' download style='display:inline-flex;align-items:center;gap:6px;padding:10px 24px;background:linear-gradient(135deg,#89b4fa,#cba6f7);color:#1e1e2e;border-radius:8px;text-decoration:none;font-size:13px;font-weight:700;'>↓ 下载文件</a>" +
                    "</div>";
            });
    }

    function previewFile(filename, token) {
        var url     = "/api/download?file=" + encodeURIComponent(filename) + "&token=" + encodeURIComponent(token);
        var convertUrl = "/api/convert?file=" + encodeURIComponent(filename) + "&token=" + encodeURIComponent(token);
        var ext     = getExt(filename);
        var content = document.getElementById("preview-content");
        var header  = document.getElementById("preview-content-header");
        var fnSpan  = document.getElementById("preview-content-filename");
        var openLnk = document.getElementById("preview-content-openlink");

        if (!content) return;
        var editBtn = document.getElementById("preview-edit-btn");
        if (editBtn) editBtn.style.display = "none";
        if (header) header.style.display = "flex";
        if (fnSpan) fnSpan.textContent   = filename;
        if (openLnk) openLnk.href        = url;

        content.style.cssText = "flex:1;overflow:auto;display:flex;align-items:center;justify-content:center;min-height:0;background:#11111b;";
        content.innerHTML = "<div style='color:#89b4fa;font-size:13px;'>⏳ 加载中...</div>";

        if (IMAGE_EXTS_SET[ext]) {
            content.innerHTML = "";
            content.style.cssText = "flex:1;overflow:auto;display:flex;align-items:center;justify-content:center;min-height:0;background:#0d0d1a;padding:8px;";
            var img = document.createElement("img");
            img.src   = url; img.alt = filename;
            img.style.cssText = "max-width:100%;max-height:100%;object-fit:contain;cursor:pointer;";
            img.title = "点击在新标签页打开原图";
            img.onclick = function() { window.open(url, "_blank"); };
            img.onerror = function() { content.innerHTML = "<div style='color:#f38ba8;font-size:13px;'>❌ 图片加载失败</div>"; };
            content.appendChild(img);

        } else if (PDF_EXTS_SET[ext]) {
            content.style.cssText = "flex:1;overflow:hidden;display:flex;flex-direction:column;min-height:0;";
            content.innerHTML = "<div style='padding:16px;color:#89b4fa;font-size:13px;'>⏳ 加载 PDF...</div>";
            // Fetch as blob to bypass X-Frame-Options: DENY
            fetch(url)
                .then(function(r) { if (!r.ok) throw new Error("HTTP " + r.status); return r.blob(); })
                .then(function(blob) {
                    var blobUrl = URL.createObjectURL(blob);
                    content.innerHTML = "";
                    content.style.cssText = "flex:1;overflow:hidden;display:flex;flex-direction:column;min-height:0;";
                    var iframe = document.createElement("iframe");
                    iframe.src   = blobUrl;
                    iframe.title = filename;
                    iframe.style.cssText = "flex:1;width:100%;border:none;background:#fff;min-height:0;";
                    content.appendChild(iframe);
                })
                .catch(function(e) {
                    content.style.cssText = "flex:1;overflow:auto;display:flex;align-items:center;justify-content:center;min-height:0;padding:16px;";
                    content.innerHTML = "<div style='text-align:center;'>" +
                        "<div style='font-size:48px;margin-bottom:12px;'>📕</div>" +
                        "<div style='color:#f38ba8;font-size:13px;margin-bottom:16px;'>PDF 加载失败: " + e.message + "</div>" +
                        "<a href='" + url + "' download style='display:inline-flex;align-items:center;gap:6px;padding:10px 24px;background:linear-gradient(135deg,#89b4fa,#cba6f7);color:#1e1e2e;border-radius:8px;text-decoration:none;font-size:13px;font-weight:700;'>↓ 下载 PDF</a>" +
                        "</div>";
                });

        } else if (ext === "docx" && typeof docx !== "undefined") {
            content.style.cssText = "flex:1;overflow:auto;min-height:0;background:#525659;padding:12px 8px;";
            content.innerHTML = "<div style='padding:16px;color:#89b4fa;font-size:13px;'>⏳ 正在本地高保真渲染 Word 文档...</div>";
            fetch(url)
                .then(function(r) { if (!r.ok) throw new Error("HTTP " + r.status); return r.arrayBuffer(); })
                .then(function(ab) {
                    content.innerHTML = "";
                    var container = document.createElement("div");
                    container.style.cssText = "width:100%;min-height:100%;box-sizing:border-box;";
                    content.appendChild(container);
                    return docx.renderAsync(ab, container, null, {
                        inWrapper: true,
                        ignoreWidth: false,
                        ignoreHeight: false,
                        debug: false
                    });
                })
                .catch(function(e) {
                    renderOfficeConversion(url, convertUrl, filename, ext, content);
                });

        } else if ((ext === "xlsx" || ext === "xls" || ext === "csv") && typeof XLSX !== "undefined") {
            content.style.cssText = "flex:1;overflow:hidden;display:flex;flex-direction:column;min-height:0;background:#11111b;";
            content.innerHTML = "<div style='padding:16px;color:#89b4fa;font-size:13px;'>⏳ 正在本地高保真渲染 Excel 表格...</div>";
            fetch(url)
                .then(function(r) { if (!r.ok) throw new Error("HTTP " + r.status); return r.arrayBuffer(); })
                .then(function(ab) {
                    renderExcelViewer(ab, filename, content);
                })
                .catch(function(e) {
                    renderOfficeConversion(url, convertUrl, filename, ext, content);
                });

        } else if (OFFICE_EXTS_SET[ext]) {
            renderOfficeConversion(url, convertUrl, filename, ext, content);

        } else if (TEXT_EXTS_SET[ext] || ext === "") {
            content.style.cssText = "flex:1;overflow:auto;min-height:0;";
            content.innerHTML = "<div style='padding:16px;color:#89b4fa;font-size:13px;'>⏳ 读取文件...</div>";
            fetch(url)
                .then(function(r) { if (!r.ok) throw new Error("HTTP " + r.status); return r.text(); })
                .then(function(text) {
                    try {
                        renderTextEditor(text, filename, url, content, fnSpan);
                    } catch(err) {
                        content.style.cssText = "flex:1;overflow:auto;display:flex;align-items:center;justify-content:center;min-height:0;";
                        content.innerHTML = "<div style='color:#f38ba8;font-size:13px;'>❌ 渲染失败: " + err.message + "</div>";
                    }
                })
                .catch(function(e) {
                    content.style.cssText = "flex:1;overflow:auto;display:flex;align-items:center;justify-content:center;min-height:0;";
                    content.innerHTML = "<div style='color:#f38ba8;font-size:13px;'>❌ 读取失败: " + e.message + "</div>";
                });

        } else {
            // Unsupported format: show file info + download button
            content.style.cssText = "flex:1;overflow:auto;display:flex;align-items:center;justify-content:center;min-height:0;padding:16px;";
            var fileIcon = getFileIcon(filename);
            var extLabel = ext.toUpperCase() || "未知格式";
            content.innerHTML =
                "<div style='text-align:center;'>" +
                "<div style='font-size:52px;margin-bottom:12px;'>" + fileIcon + "</div>" +
                "<div style='color:#cdd6f4;font-size:13px;font-weight:600;margin-bottom:4px;word-break:break-all;'>" + filename + "</div>" +
                "<div style='color:#6c7086;font-size:12px;margin-bottom:20px;'>" + extLabel + " · 暂不支持在线预览</div>" +
                "<a href='" + url + "' download style='display:inline-flex;align-items:center;gap:6px;padding:10px 24px;background:linear-gradient(135deg,#89b4fa,#cba6f7);color:#1e1e2e;border-radius:8px;text-decoration:none;font-size:13px;font-weight:700;'>↓ 下载文件</a>" +
                "</div>";
        }
    }

    function addImagePreview(filename, filepath, token) {
        // Open panel and auto-preview the uploaded image
        var panel = document.getElementById("preview-panel");
        var handle = document.getElementById("panel-resize-handle");
        if (panel && panel.style.display === "none") {
            panel.style.display = "flex";
            if (handle) handle.style.display = "block";
            var toggleBtn = document.getElementById("preview-toggle-btn");
            if (toggleBtn) toggleBtn.style.background = "#45475a";
            startFileBrowserRefresh();
            if (activeTermId && terminals[activeTermId]) terminals[activeTermId].fitAddon.fit();
        }
        // Load file list first, then preview
        loadFileList();
        previewFile(filename, token);
        // Highlight the file in the list after load
        setTimeout(function() {
            var list = document.getElementById("file-list");
            if (!list) return;
            list.querySelectorAll(".fb-selected").forEach(function(el) { el.classList.remove("fb-selected"); el.style.background = ""; });
            var rows = list.querySelectorAll("[data-name]");
            rows.forEach(function(row) {
                if (row.dataset.name === filename) {
                    row.classList.add("fb-selected");
                    row.style.background = "#45475a";
                    row.scrollIntoView({ behavior: "smooth", block: "nearest" });
                }
            });
        }, 400);
    }

    function showProgress(filename) {
        var c = document.getElementById("upload-progress-container");
        var bar = document.getElementById("upload-bar");
        var pct = document.getElementById("upload-percent");
        var fn = document.getElementById("upload-filename");
        var st = document.getElementById("upload-status");
        if (!c) return;
        fn.textContent = filename;
        bar.style.width = "0%";
        pct.textContent = "0%";
        st.textContent = "\u4e0a\u4f20\u4e2d...";
        st.style.color = "#89b4fa";
        c.style.display = "block";
    }

    function updateProgress(loaded, total) {
        var bar = document.getElementById("upload-bar");
        var pct = document.getElementById("upload-percent");
        if (!bar) return;
        var p = total > 0 ? Math.round((loaded / total) * 100) : 0;
        bar.style.width = p + "%";
        pct.textContent = p + "%";
    }

    function hideProgress(success, msg) {
        var c = document.getElementById("upload-progress-container");
        var bar = document.getElementById("upload-bar");
        var pct = document.getElementById("upload-percent");
        var st = document.getElementById("upload-status");
        if (!c) return;
        bar.style.width = "100%";
        pct.textContent = "100%";
        st.textContent = success ? ("\u2705 " + (msg || "\u4e0a\u4f20\u6210\u529f")) : ("\u274c " + (msg || "\u4e0a\u4f20\u5931\u8d25"));
        st.style.color = success ? "#a6e3a1" : "#f38ba8";
        setTimeout(function() { c.style.display = "none"; }, success ? 2000 : 3000);
    }

    function uploadFileAndInsertPath(file) {
        if (!file) return;
        showProgress(file.name);

        var formData = new FormData();
        formData.append("file", file);

        var token = SessionManager.getToken();
        var uploadUrl = "/api/upload?token=" + encodeURIComponent(token);

        var xhr = new XMLHttpRequest();
        xhr.open("POST", uploadUrl, true);

        xhr.upload.onprogress = function(e) {
            if (e.lengthComputable) updateProgress(e.loaded, e.total);
        };

        xhr.onload = function() {
            if (xhr.status === 200) {
                try {
                    var data = JSON.parse(xhr.responseText);
                    var filepath = data.path || ("./transfers/" + data.filename);
                    if (activeTermId && terminals[activeTermId] && terminals[activeTermId].ws) {
                        var ws = terminals[activeTermId].ws;
                        if (ws.readyState === WebSocket.OPEN) {
                            ws.send(filepath + " ");
                        }
                    }
                    hideProgress(true, "\u4e0a\u4f20\u6210\u529f: " + data.filename);
                    // 如果是图片，自动弹出预览
                    if (IMAGE_EXTS.test(data.filename)) {
                        addImagePreview(data.filename, filepath, token);
                    }
                    setStatus("Connected");
                } catch(e) {
                    hideProgress(false, "\u89e3\u6790\u54cd\u5e94\u5931\u8d25");
                }
            } else {
                hideProgress(false, "HTTP " + xhr.status);
                setStatus("Upload error: " + xhr.status);
                setTimeout(function() { setStatus("Connected"); }, 3000);
            }
        };

        xhr.onerror = function() {
            hideProgress(false, "\u7f51\u7edc\u9519\u8bef");
            setStatus("Upload error: network error");
            setTimeout(function() { setStatus("Connected"); }, 3000);
        };

        xhr.send(formData);
    }

    function setupDragAndDropAndPaste() {
        document.body.addEventListener("dragover", function (e) {
            e.preventDefault();
            e.stopPropagation();
        });

        document.body.addEventListener("drop", function (e) {
            e.preventDefault();
            e.stopPropagation();
            if (e.dataTransfer && e.dataTransfer.files && e.dataTransfer.files.length > 0) {
                for (var i = 0; i < e.dataTransfer.files.length; i++) {
                    uploadFileAndInsertPath(e.dataTransfer.files[i]);
                }
            }
        });

        // Ctrl+V: 截图粘贴（对图片数据最有效）
        window.addEventListener("paste", function (e) {
            var cd = e.clipboardData || (e.originalEvent && e.originalEvent.clipboardData);
            if (!cd) return;
            var hasFile = false;
            for (var i = 0; i < cd.items.length; i++) {
                if (cd.items[i].kind === "file") {
                    hasFile = true;
                    var blob = cd.items[i].getAsFile();
                    if (!blob) continue;
                    var ext = (blob.type && blob.type.split("/")[1]) || "png";
                    var filename = blob.name && blob.name !== "image.png"
                        ? blob.name
                        : "pasted_" + Date.now() + "." + ext;
                    var file = new File([blob], filename, { type: blob.type });
                    uploadFileAndInsertPath(file);
                }
            }
            if (hasFile) e.preventDefault();
        });

        // Ctrl+Shift+V: 使用 Clipboard API 读取当前剪贴板文件（支持图片等）
        document.addEventListener("keydown", function (e) {
            if (e.ctrlKey && e.shiftKey && (e.key === "V" || e.key === "v")) {
                e.preventDefault();
                if (!navigator.clipboard || !navigator.clipboard.read) {
                    setStatus("\u6d4f\u89c8\u5668\u4e0d\u652f\u6301 Clipboard API");
                    setTimeout(function() { setStatus("Connected"); }, 2000);
                    return;
                }
                navigator.clipboard.read().then(function (items) {
                    var found = false;
                    for (var i = 0; i < items.length; i++) {
                        (function(item) {
                            var types = item.types;
                            for (var j = 0; j < types.length; j++) {
                                var t = types[j];
                                if (t === "text/plain") continue; // 跳过纯文本
                                found = true;
                                (function(type) {
                                    item.getType(type).then(function (blob) {
                                        var ext = type.split("/")[1] || "bin";
                                        ext = ext.replace(/[^a-z0-9]/gi, "") || "bin";
                                        var file = new File([blob], "clipboard_" + Date.now() + "." + ext, { type: type });
                                        uploadFileAndInsertPath(file);
                                    }).catch(function() {});
                                })(t);
                            }
                        })(items[i]);
                    }
                    if (!found) {
                        setStatus("\u526a\u8d34\u677f\u4e2d\u6ca1\u6709\u6587\u4ef6");
                        setTimeout(function() { setStatus("Connected"); }, 2000);
                    }
                }).catch(function (err) {
                    setStatus("\u526a\u8d34\u677f\u8bfb\u53d6\u5931\u8d25: " + err.message);
                    setTimeout(function() { setStatus("Connected"); }, 3000);
                });
            }
        });
    }

    function setupVoiceInput() {
        var voiceBtn = document.getElementById("voice-btn");
        var panel = document.getElementById("voice-panel");
        var panelHead = document.getElementById("voice-panel-head");
        var panelBody = document.getElementById("voice-panel-body");
        var collapseBtn = document.getElementById("voice-panel-collapse");
        var modalClose = document.getElementById("voice-modal-close");
        var transcriptText = document.getElementById("voice-transcript-text");
        var toggleRecBtn = document.getElementById("voice-toggle-rec-btn");
        var recDot = document.getElementById("rec-dot");
        var recBtnText = document.getElementById("rec-btn-text");
        var autoSendCheckbox = document.getElementById("voice-auto-send");
        var clearBtn = document.getElementById("voice-clear-btn");
        var sendBtn = document.getElementById("voice-send-btn");
        var statusIcon = document.getElementById("voice-status-icon");
        var statusText = document.getElementById("voice-status-text");

        if (!voiceBtn || !panel) return;

        var engineSelect = document.getElementById("voice-engine");
        var SpeechRecognition = window.SpeechRecognition || window.webkitSpeechRecognition;

        // ONE engine runs at a time. The two engines used to race, both writing
        // this same textarea, with a 5s cross-fade window -- on a 2-core CPU that
        // guaranteed garbled/stalled text. Now: pick one, fall back only on hard
        // failure, never both at once.
        var AUTOSEND_MS = 2200;   // silence before auto-send fires
        var VAD_TICK_MS = 100;    // RMS sampling period
        var VAD_SILENCE_MS = 700; // silence that closes an utterance
        var VAD_MIN_MS = 400;     // ignore blips shorter than this
        var VAD_MAX_MS = 12000;   // hard cap: force-close a long utterance
        var VAD_ON = 0.020;       // RMS above -> speech
        var VAD_OFF = 0.012;      // RMS below -> silence (hysteresis)

        var mode = '';            // 'native' | 'server' — the engine now running
        var isRecording = false;
        var stopping = false;
        var silenceTimer = null;
        var lastAutoText = '';

        // --- native engine state ---
        var recognition = null;
        var natCarry = '';        // finals from *previous* recognition sessions
        var natFinal = '';        // finals of the current session
        var natInterim = '';      // in-progress text of the current session
        var natConsumed = 0;      // # of final results already sent to terminal
        var natFinalCount = 0;    // # of final results seen in this session
        var natRestart = null;
        var natBackoff = 0;
        var natGotAny = false;    // native produced text at least once

        // --- server engine state ---
        var micStream = null;
        var mediaRecorder = null;
        var audioCtx = null, analyser = null, vadTimer = null;
        var vadBuf = null;
        var speaking = false, speechStart = 0, lastVoice = 0;
        var srvText = '';         // committed transcript from finished utterances
        var srvSeq = 0, srvNextEmit = 1, srvPending = {}, srvQueue = 0;

        function setStatus(msg, color) {
            statusText.textContent = msg;
            if (color) statusText.style.color = color;
        }

        function joinText(a, b) {
            if (!a) return b || '';
            if (!b) return a;
            return a + b;
        }

        // Single writer for the textarea. Whichever engine owns `mode` decides
        // the content, so the two can never interleave into garbage.
        function render() {
            var full = (mode === 'native')
                ? joinText(joinText(natCarry, natFinal), natInterim)
                : srvText;
            if (transcriptText.value !== full) {
                transcriptText.value = full;
                transcriptText.scrollTop = transcriptText.scrollHeight;
            }
            scheduleAutoSend(full);
        }

        // Called after the text is handed to the terminal (auto-send / 发送 / 清除).
        //
        // `sentInterim` means the dispatched text included the in-progress
        // utterance. Interim text must be cleared either way -- keeping it made
        // render() paste the previous sentence straight back into the textarea,
        // so a second dictation always started with stale content. But if it was
        // already sent we must also swallow its eventual final result, or the
        // same words reach the terminal twice.
        function consumeText(sentInterim) {
            if (mode === 'native') {
                natCarry = '';
                natFinal = '';
                // Chrome reports results cumulatively, so onNativeResult skips
                // the first natConsumed finals when rebuilding the text.
                natConsumed = (sentInterim && natInterim.trim())
                    ? natFinalCount + 1
                    : natFinalCount;
                natInterim = '';
            } else {
                srvText = '';
            }
            transcriptText.value = '';
            lastAutoText = '';
            if (silenceTimer) { clearTimeout(silenceTimer); silenceTimer = null; }
        }

        function updateRecordingUI(recording) {
            isRecording = recording;
            if (recording) {
                statusIcon.textContent = "🎙️";
                statusText.textContent = (mode === 'native')
                    ? "正在倾听中... (原生实时)"
                    : "正在倾听中... (本机 Whisper)";
                statusText.style.color = "#f38ba8";
                recBtnText.textContent = "停止录音";
                recDot.style.background = "#11111b";
                toggleRecBtn.style.background = "#a6e3a1";
                voiceBtn.textContent = "🔴 录音中";
                voiceBtn.style.borderColor = "#f38ba8";
            } else {
                statusIcon.textContent = "🎤";
                statusText.textContent = "语音开发助手 (就绪)";
                statusText.style.color = "#cdd6f4";
                recBtnText.textContent = "开始录音";
                recDot.style.background = "#11111b";
                toggleRecBtn.style.background = "#f38ba8";
                voiceBtn.textContent = "🎤 语音";
                voiceBtn.style.borderColor = "#45475a";
            }
        }

        function releaseMic() {
            if (vadTimer) { clearInterval(vadTimer); vadTimer = null; }
            if (audioCtx) {
                try { audioCtx.close(); } catch (e) {}
                audioCtx = null; analyser = null; vadBuf = null;
            }
            if (micStream) {
                micStream.getTracks().forEach(function (t) { try { t.stop(); } catch (e) {} });
                micStream = null;
            }
        }

        function stopNative() {
            if (natRestart) { clearTimeout(natRestart); natRestart = null; }
            if (recognition) {
                recognition.onend = null;
                recognition.onresult = null;
                recognition.onerror = null;
                try { recognition.abort(); } catch (e) {}
                recognition = null;
            }
        }

        function stopRecording() {
            isRecording = false;
            stopping = true;
            stopNative();
            if (silenceTimer) { clearTimeout(silenceTimer); silenceTimer = null; }

            if (mediaRecorder && mediaRecorder.state !== "inactive") {
                // Flush the tail of the current utterance, then release the mic.
                try { mediaRecorder.stop(); } catch (e) {}
            }
            mediaRecorder = null;
            speaking = false;
            releaseMic();
            updateRecordingUI(false);
        }

        // One POST per *finished utterance*. The old code re-posted the whole
        // growing segment every 1.5s, so backend work grew with the square of
        // the segment length -- fatal when the model's RTF is already ~1.
        function postUtterance(blob, mime) {
            if (!blob || blob.size < 1200) return;
            var seq = ++srvSeq;
            var ext = (mime && mime.indexOf('mp4') !== -1) ? 'mp4' : 'webm';
            var fd = new FormData();
            fd.append("audio", blob, "chunk." + ext);

            srvQueue++;
            showQueue();

            fetch("/api/voice/transcribe", { method: "POST", body: fd })
                .then(function (r) { return r.json(); })
                .then(function (data) {
                    srvQueue--;
                    if (data && data.error) {
                        setStatus("转写错误: " + data.error, "#f9e2af");
                        srvPending[seq] = '';
                    } else {
                        srvPending[seq] = (data && data.text || '').trim();
                    }
                    drainPending();
                    showQueue();
                })
                .catch(function (err) {
                    srvQueue--;
                    console.error("[voice] transcribe failed:", err);
                    setStatus("后台转写请求失败", "#f9e2af");
                    srvPending[seq] = '';
                    drainPending();
                    showQueue();
                });
        }

        // Utterances may come back out of order; emit strictly in spoken order.
        function drainPending() {
            var changed = false;
            while (Object.prototype.hasOwnProperty.call(srvPending, srvNextEmit)) {
                var t = srvPending[srvNextEmit];
                delete srvPending[srvNextEmit];
                srvNextEmit++;
                if (t) { srvText = joinText(srvText, t); changed = true; }
            }
            if (changed) render();
        }

        function showQueue() {
            if (!isRecording || mode !== 'server') return;
            setStatus(srvQueue > 0
                ? "转写中... (排队 " + srvQueue + " 句)"
                : "正在倾听中... (本机 Whisper)", "#f38ba8");
        }

        // Single exit point for "this text is done, hand it over and reset".
        // Whether the text still carried an interim tail decides if that tail's
        // final result has to be swallowed, so the two must be decided together.
        //
        // With AI mode on the text goes to the model instead of the shell, and
        // the resulting command lands in the review bar for the user to confirm.
        function dispatchText(text) {
            var sentInterim = (mode === 'native') && !!natInterim.trim() &&
                text.indexOf(natInterim.trim()) !== -1;
            if (aiEnabled()) {
                translateAndSend(text);
            } else {
                sendToTerminal(text, true);
            }
            consumeText(sentInterim);
        }

        // Only (re)arm the timer when the text actually grew. Re-arming on every
        // identical poll is what previously kept auto-send from ever firing.
        function scheduleAutoSend(text) {
            if (!autoSendCheckbox || !autoSendCheckbox.checked) return;
            if (!isRecording) return;
            var t = (text || '').trim();
            if (!t) return;
            if (t === lastAutoText && silenceTimer) return;
            lastAutoText = t;
            if (silenceTimer) clearTimeout(silenceTimer);
            silenceTimer = setTimeout(function () {
                silenceTimer = null;
                var out = transcriptText.value.trim();
                if (!out || !isRecording) return;
                // Never auto-send a half-formed utterance.
                if (mode === 'native' && natInterim.trim() && !natFinal.trim() && !natCarry.trim()) {
                    scheduleAutoSend(out);
                    return;
                }
                dispatchText(out);
            }, AUTOSEND_MS);
        }

        function pickMime() {
            var cands = ['audio/webm;codecs=opus', 'audio/webm', 'audio/mp4', ''];
            for (var i = 0; i < cands.length; i++) {
                try {
                    if (!cands[i] || !window.MediaRecorder.isTypeSupported ||
                        window.MediaRecorder.isTypeSupported(cands[i])) return cands[i];
                } catch (e) {}
            }
            return '';
        }

        // Record ONE utterance: start on speech onset, stop on silence. Each
        // recorder instance produces exactly one self-contained (decodable)
        // WebM/MP4 file, so there is never a partial-stream or re-send problem.
        function startUtterance() {
            if (!micStream || mediaRecorder) return;
            var mime = pickMime();
            var rec;
            try {
                rec = mime ? new MediaRecorder(micStream, { mimeType: mime })
                           : new MediaRecorder(micStream);
            } catch (e) {
                console.error("[voice] MediaRecorder init failed:", e);
                return;
            }
            var chunks = [];

            rec.ondataavailable = function (event) {
                if (event.data && event.data.size > 0) chunks.push(event.data);
            };
            rec.onstop = function () {
                var keep = !rec.__drop && chunks.length > 0;
                if (keep) postUtterance(new Blob(chunks, { type: rec.mimeType || 'audio/webm' }), rec.mimeType);
                chunks = [];
                if (stopping) setStatus("语音开发助手 (就绪)", "#cdd6f4");
            };
            rec.onerror = function (e) { console.error("[voice] MediaRecorder error:", e); };

            mediaRecorder = rec;
            speechStart = Date.now();
            try { rec.start(); } catch (e) { mediaRecorder = null; }
        }

        function endUtterance(drop) {
            var rec = mediaRecorder;
            mediaRecorder = null;
            if (!rec) return;
            if (drop) rec.__drop = true;
            if (rec.state !== "inactive") {
                try { rec.stop(); } catch (e) {}
            }
        }

        function resetState() {
            stopping = false;
            natCarry = ''; natFinal = ''; natInterim = '';
            natConsumed = 0; natFinalCount = 0; natBackoff = 0; natGotAny = false;
            srvText = ''; srvSeq = 0; srvNextEmit = 1; srvPending = {}; srvQueue = 0;
            lastAutoText = '';
            speaking = false;
            transcriptText.value = '';
            if (silenceTimer) { clearTimeout(silenceTimer); silenceTimer = null; }
        }

        function startRecording() {
            resetState();
            var want = engineSelect ? engineSelect.value : 'auto';
            if (want === 'native' || (want === 'auto' && SpeechRecognition)) {
                if (!SpeechRecognition) {
                    setStatus("此浏览器无原生识别引擎，请选『本机 Whisper』", "#f9e2af");
                    return;
                }
                mode = 'native';
                startNative();
            } else {
                mode = 'server';
                startServer();
            }
        }

        // ---------- native engine ----------
        function startNative() {
            var r;
            try {
                r = new SpeechRecognition();
            } catch (e) {
                console.error("[voice] native init failed:", e);
                fallbackToServer("原生引擎无法启动");
                return;
            }
            recognition = r;
            r.continuous = true;
            r.interimResults = true;
            r.lang = 'zh-CN';
            r.maxAlternatives = 1;
            r.onresult = onNativeResult;
            r.onerror = onNativeError;
            r.onend = onNativeEnd;
            try {
                r.start();
                updateRecordingUI(true);
            } catch (e) {
                console.error("[voice] native start failed:", e);
                fallbackToServer("原生引擎启动被拒绝");
            }
        }

        // Rebuild the session text from scratch on every event. Chrome delivers
        // `event.results` cumulatively, so rebuilding is the only way that stays
        // correct across a mid-session send.
        function onNativeResult(event) {
            var finals = 0, fin = '', interim = '';
            for (var i = 0; i < event.results.length; ++i) {
                var res = event.results[i];
                var chunk = res[0] ? res[0].transcript : '';
                if (res.isFinal) {
                    finals++;
                    if (finals > natConsumed) fin += chunk;
                } else {
                    interim += chunk;
                }
            }
            natFinalCount = finals;
            natFinal = fin;
            natInterim = interim;
            if (fin.trim() || interim.trim()) natGotAny = true;
            natBackoff = 0;
            if (isRecording) setStatus("正在倾听中... (原生实时)", "#f38ba8");
            render();
        }

        function onNativeError(event) {
            var err = event && event.error;
            console.warn("[voice] native engine error:", err);
            if (err === 'no-speech' || err === 'aborted') return; // onend will restart
            if (err === 'not-allowed' || err === 'service-not-allowed') {
                setStatus("麦克风被拒绝：请允许权限并确认使用 HTTPS", "#f38ba8");
                stopRecording();
                return;
            }
            // 'network' / 'language-not-supported': the native engine is unusable
            // here. Hand over once instead of flapping between engines.
            if (isRecording && (engineSelect ? engineSelect.value : 'auto') === 'auto' && !natGotAny) {
                fallbackToServer("原生引擎不可用 (" + err + ")");
            }
        }

        // Chrome ends the session on silence. Restart it, but carry the text over
        // first -- the old code reset its counters and let the next session
        // overwrite everything already transcribed.
        function onNativeEnd() {
            if (!isRecording || !recognition) return;
            natCarry = joinText(natCarry, natFinal);
            natFinal = '';
            natInterim = '';
            natConsumed = 0;
            natFinalCount = 0;
            var delay = Math.min(1000, natBackoff);
            natBackoff = natBackoff ? Math.min(1000, natBackoff * 2) : 120;
            if (natRestart) clearTimeout(natRestart);
            natRestart = setTimeout(function () {
                natRestart = null;
                if (!isRecording || !recognition) return;
                try { recognition.start(); } catch (e) { /* already starting */ }
            }, delay);
        }

        function fallbackToServer(why) {
            stopNative();
            mode = 'server';
            natCarry = ''; natFinal = ''; natInterim = '';
            setStatus(why + "，已切到本机 Whisper", "#f9e2af");
            startServer();
        }

        // ---------- server engine (VAD-segmented) ----------
        function startServer() {
            if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
                alert("当前环境不支持录音 (需要 HTTPS 或 localhost)");
                return;
            }
            if (!window.MediaRecorder) {
                setStatus("此浏览器不支持 MediaRecorder", "#f38ba8");
                return;
            }
            if (micStream) { updateRecordingUI(true); return; }

            navigator.mediaDevices.getUserMedia({
                audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true }
            }).then(function (stream) {
                micStream = stream;
                updateRecordingUI(true);
                startVAD(stream);
            }).catch(function (err) {
                console.error("录音授权失败:", err);
                alert("麦克风授权失败: " + err.message);
                stopRecording();
            });
        }

        function startVAD(stream) {
            var AC = window.AudioContext || window.webkitAudioContext;
            if (!AC) { // no WebAudio: fall back to fixed 4s slices
                fixedSlices();
                return;
            }
            try {
                audioCtx = new AC();
                var src = audioCtx.createMediaStreamSource(stream);
                analyser = audioCtx.createAnalyser();
                analyser.fftSize = 1024;
                src.connect(analyser);
                vadBuf = new Float32Array(analyser.fftSize);
            } catch (e) {
                console.warn("[voice] VAD unavailable, using fixed slices:", e);
                fixedSlices();
                return;
            }
            if (vadTimer) clearInterval(vadTimer);
            vadTimer = setInterval(vadTick, VAD_TICK_MS);
        }

        function rms() {
            if (!analyser || !vadBuf) return 0;
            if (analyser.getFloatTimeDomainData) {
                analyser.getFloatTimeDomainData(vadBuf);
            } else { return 0; }
            var sum = 0;
            for (var i = 0; i < vadBuf.length; i++) sum += vadBuf[i] * vadBuf[i];
            return Math.sqrt(sum / vadBuf.length);
        }

        function vadTick() {
            if (!isRecording || mode !== 'server') return;
            var now = Date.now();
            var level = rms();

            if (!speaking) {
                if (level > VAD_ON) {
                    speaking = true;
                    lastVoice = now;
                    startUtterance();
                }
                return;
            }

            if (level > VAD_OFF) lastVoice = now;

            // Force-close a monologue so one POST never carries minutes of audio.
            if (now - speechStart > VAD_MAX_MS) {
                speaking = false;
                endUtterance(false);
                return;
            }
            if (now - lastVoice > VAD_SILENCE_MS) {
                speaking = false;
                // Too short to be speech -> discard instead of paying to decode it.
                endUtterance((now - speechStart) < VAD_MIN_MS);
            }
        }

        // Last-resort path when WebAudio is missing: 4s fixed utterances.
        function fixedSlices() {
            if (vadTimer) clearInterval(vadTimer);
            speaking = true;
            startUtterance();
            vadTimer = setInterval(function () {
                if (!isRecording || mode !== 'server') return;
                endUtterance(false);
                startUtterance();
            }, 4000);
        }


        function focusTerminal() {
            if (activeTermId && terminals[activeTermId] && terminals[activeTermId].term) {
                try { terminals[activeTermId].term.focus(); } catch (e) {}
            }
        }

        window.openVoiceModal = function () {
            if (panel.style.display !== "flex") {
                panel.style.display = "flex";
                restorePanelPos();
            }
            if (!isRecording) startRecording();
            // Keep typing in the terminal: the panel must never steal focus.
            focusTerminal();
        };

        function closeVoicePanel() {
            panel.style.display = "none";
            stopRecording();
            focusTerminal();
        }

        voiceBtn.onclick = window.openVoiceModal;
        modalClose.addEventListener("click", closeVoicePanel);

        // Collapse to just the title bar; recording keeps running.
        var collapsed = false;
        function setCollapsed(v) {
            collapsed = v;
            panelBody.style.display = v ? "none" : "flex";
            collapseBtn.textContent = v ? "+" : "−";
            collapseBtn.title = v ? "展开" : "折叠";
            try { localStorage.setItem("voicePanelCollapsed", v ? "1" : "0"); } catch (e) {}
        }
        collapseBtn.addEventListener("click", function (e) {
            e.stopPropagation();
            setCollapsed(!collapsed);
            focusTerminal();
        });
        try {
            if (localStorage.getItem("voicePanelCollapsed") === "1") setCollapsed(true);
        } catch (e) {}

        // Escape closes the panel only while focus is inside it, so Escape still
        // reaches vim/less in the terminal.
        panel.addEventListener("keydown", function (e) {
            if (e.key === "Escape") {
                e.stopPropagation();
                closeVoicePanel();
            }
        });

        // --- Drag the panel by its title bar -------------------------------
        var dragging = false, dragDX = 0, dragDY = 0;

        function clampToViewport(left, top) {
            var w = panel.offsetWidth, h = panel.offsetHeight;
            var maxL = Math.max(0, window.innerWidth - w);
            var maxT = Math.max(0, window.innerHeight - h);
            return {
                left: Math.min(Math.max(0, left), maxL),
                top: Math.min(Math.max(0, top), maxT)
            };
        }

        function placePanel(left, top) {
            var p = clampToViewport(left, top);
            panel.style.left = p.left + "px";
            panel.style.top = p.top + "px";
            panel.style.right = "auto";
            panel.style.bottom = "auto";
            try {
                localStorage.setItem("voicePanelPos", p.left + "," + p.top);
            } catch (e) {}
        }

        function restorePanelPos() {
            var saved = null;
            try { saved = localStorage.getItem("voicePanelPos"); } catch (e) {}
            if (!saved) return; // keep the default bottom-right anchor
            var parts = saved.split(",");
            var l = parseInt(parts[0], 10), t = parseInt(parts[1], 10);
            if (isNaN(l) || isNaN(t)) return;
            placePanel(l, t);
        }

        panelHead.addEventListener("pointerdown", function (e) {
            if (e.target.tagName === "BUTTON") return; // let buttons work
            dragging = true;
            var r = panel.getBoundingClientRect();
            dragDX = e.clientX - r.left;
            dragDY = e.clientY - r.top;
            try { panelHead.setPointerCapture(e.pointerId); } catch (err) {}
        });

        panelHead.addEventListener("pointermove", function (e) {
            if (!dragging) return;
            e.preventDefault();
            placePanel(e.clientX - dragDX, e.clientY - dragDY);
        });

        function endDrag(e) {
            if (!dragging) return;
            dragging = false;
            try { panelHead.releasePointerCapture(e.pointerId); } catch (err) {}
        }
        panelHead.addEventListener("pointerup", endDrag);
        panelHead.addEventListener("pointercancel", endDrag);

        // Keep the panel reachable when the window shrinks.
        window.addEventListener("resize", function () {
            if (panel.style.display !== "flex") return;
            if (!panel.style.left) return; // still anchored bottom-right
            placePanel(parseInt(panel.style.left, 10) || 0,
                       parseInt(panel.style.top, 10) || 0);
        });

        toggleRecBtn.addEventListener("click", function () {
            if (isRecording) {
                stopRecording();
            } else {
                startRecording();
            }
        });

        clearBtn.addEventListener("click", function () {
            // Nothing was sent, so the interim tail's final result is unwanted
            // too -- treat it as consumed.
            consumeText(true);
            focusTerminal();
        });

        sendBtn.addEventListener("click", function () {
            var text = transcriptText.value.trim();
            if (!text) return;
            dispatchText(text);
            focusTerminal();
        });

        // Switching engine mid-recording restarts on the new one.
        if (engineSelect) {
            try {
                var savedEng = localStorage.getItem("voiceEngine");
                if (savedEng) engineSelect.value = savedEng;
            } catch (e) {}
            engineSelect.addEventListener("change", function () {
                try { localStorage.setItem("voiceEngine", engineSelect.value); } catch (e) {}
                if (isRecording) {
                    stopRecording();
                    setTimeout(startRecording, 250);
                }
                focusTerminal();
            });
        }

        function sendToTerminal(text, sendEnter) {
            if (!activeTermId || !terminals[activeTermId]) {
                alert("请先连接或选中终端标签页");
                return;
            }
            var ws = terminals[activeTermId].ws;
            if (ws && ws.readyState === WebSocket.OPEN) {
                ws.send(text + (sendEnter ? "\r" : ""));
            }
        }
    }

    // Check if auth is required
    fetch("/api/login", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" })
        .then(function (r) { return r.json(); })
        .then(function (data) {
            if (data.token === "no-auth") {
                SessionManager.login("", "").then(function () { init(); });
            } else {
                init();
            }
        })
        .catch(function () { init(); });
})();
