// appData 返回 Alpine.js 根组件的状态与方法。
// 由 index.html 的 x-data="appData()" 使用。
function appData() {
  return {
    state: { stage: '', agents: [] },
    docs: {},
    selectedDoc: 'desire',
    asks: [],
    logs: [],
    inputRequest: '',
    githubForm: { remote: '', branch: '', token: '' },
    answerInputs: {},

    // Alpine 在组件初始化时自动调用 init()。
    async init() {
      await this.fetchConfig();
      await this.fetchState();
      this.fetchDoc(this.selectedDoc);
      await this.fetchAsks();
      this.connectSSE();
      // 定时轮询。
      setInterval(() => this.fetchState(), 2000);
      setInterval(() => this.fetchAsks(), 2000);
      setInterval(() => this.fetchDoc(this.selectedDoc), 3000);
    },

    selectDoc(d) {
      this.selectedDoc = d;
      this.fetchDoc(d);
    },

    async fetchState() {
      try {
        const r = await fetch('/api/state');
        if (r.ok) this.state = await r.json();
      } catch (e) { /* 忽略瞬时网络错误 */ }
    },

    async fetchDoc(name) {
      try {
        const r = await fetch('/api/docs/' + name);
        if (r.ok) {
          this.docs[name] = await r.json();
        } else {
          this.docs[name] = null;
        }
      } catch (e) { /* 忽略 */ }
    },

    async fetchAsks() {
      try {
        const r = await fetch('/api/asks');
        if (r.ok) {
          const j = await r.json();
          this.asks = j.asks || [];
        }
      } catch (e) { /* 忽略 */ }
    },

    async fetchConfig() {
      try {
        const r = await fetch('/api/config');
        if (r.ok) {
          const j = await r.json();
          if (j.github) {
            this.githubForm.remote = j.github.remote || '';
            this.githubForm.branch = j.github.branch || '';
            this.githubForm.token = ''; // 出于安全不回显 token
          }
        }
      } catch (e) { /* 忽略 */ }
    },

    async submitInput() {
      if (!this.inputRequest.trim()) return;
      try {
        const r = await fetch('/api/input', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ request: this.inputRequest })
        });
        if (r.ok) {
          this.inputRequest = '';
          this.fetchDoc('desire');
        }
      } catch (e) { /* 忽略 */ }
    },

    async submitAnswer(id) {
      const ans = this.answerInputs[id] || '';
      try {
        const r = await fetch('/api/ask/' + id, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ answer: ans })
        });
        if (r.ok) {
          delete this.answerInputs[id];
          this.fetchAsks();
        }
      } catch (e) { /* 忽略 */ }
    },

    async submitGithub() {
      try {
        await fetch('/api/github', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(this.githubForm)
        });
      } catch (e) { /* 忽略 */ }
    },

    // SSE 连接 /api/events，断开后自动重连。
    connectSSE() {
      const es = new EventSource('/api/events');
      es.onmessage = (e) => {
        try {
          const evt = JSON.parse(e.data);
          this.logs.push(evt);
          if (this.logs.length > 200) this.logs.shift();
          if (evt.type === 'agent_start' || evt.type === 'agent_done' || evt.type === 'agent_failed') {
            this.fetchState();
          }
          if (evt.type === 'ask_user') {
            this.fetchAsks();
          }
          if (evt.type === 'doc_update') {
            this.fetchDoc(this.selectedDoc);
          }
        } catch (err) { /* 忽略解析错误 */ }
      };
      es.onerror = () => {
        es.close();
        setTimeout(() => this.connectSSE(), 3000);
      };
    },

    // ---- 样式辅助 ----
    stageBadgeClass(stage) {
      if (!stage) return 'bg-gray-200 text-gray-600';
      return 'bg-blue-100 text-blue-700';
    },
    agentRowClass(a) {
      if (a.status === 'running') return 'bg-blue-50';
      if (a.status === 'done') return 'bg-green-50';
      if (a.status === 'failed') return 'bg-red-50';
      return '';
    },
    agentDotClass(a) {
      if (a.status === 'running') return 'bg-blue-600 text-white animate-pulse';
      if (a.status === 'done') return 'bg-green-600 text-white';
      if (a.status === 'failed') return 'bg-red-600 text-white';
      return 'bg-gray-300 text-gray-600';
    },
    statusLabel(s) {
      return { pending: '待执行', running: '运行中', done: '完成', failed: '失败' }[s] || s;
    },
    formatTime(t) {
      if (!t) return '';
      try { return new Date(t).toLocaleTimeString(); } catch (e) { return t; }
    },
    formatData(d) {
      if (d == null) return '';
      if (typeof d === 'string') return d;
      try { return JSON.stringify(d); } catch (e) { return String(d); }
    }
  };
}
