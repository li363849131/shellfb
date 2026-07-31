var SessionManager = (function () {
    "use strict";

    var token = localStorage.getItem("sfb_token") || "";
    var sessions = [];
    var activeSessionId = null;

    function setToken(t) {
        token = t;
        localStorage.setItem("sfb_token", t);
    }

    function getToken() {
        return token;
    }

    function clearToken() {
        token = "";
        localStorage.removeItem("sfb_token");
    }

    async function login(username, password) {
        var resp = await fetch("/api/login", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ username: username, password: password }),
        });
        var data = await resp.json();
        if (data.error) throw new Error(data.error);
        setToken(data.token);
        return data.token;
    }

    async function listSessions() {
        var resp = await fetch("/api/sessions?token=" + encodeURIComponent(token));
        if (resp.status === 401) {
            clearToken();
            return [];
        }
        sessions = await resp.json();
        return sessions || [];
    }

    // Kill the session server-side. Closing a browser tab used to only drop the
    // WebSocket, leaving the PTY alive and still listed by every other browser.
    async function destroySession(id) {
        if (!id) return;
        await fetch("/api/sessions?id=" + encodeURIComponent(id) +
            "&token=" + encodeURIComponent(token), { method: "DELETE" });
    }

    function setActive(id) {
        activeSessionId = id;
    }

    function getActive() {
        return activeSessionId;
    }

    return {
        login: login,
        listSessions: listSessions,
        destroySession: destroySession,
        getToken: getToken,
        clearToken: clearToken,
        setActive: setActive,
        getActive: getActive,
    };
})();
