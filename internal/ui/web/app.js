// appData 返回 Alpine.js 根组件的状态与方法。
// 由 index.html 的 x-data="appData()" 使用。
//
// v0.3.0 改造为多页面 SPA：
//   - page: 'projects' / 'settings' / 'stats' / 'tasks'
//   - projects: 项目列表 + 新建项目 + 当前项目详情（流程/文档/Asker/输入/GitHub override）
//   - settings: 9 行角色模型表单
//   - stats:    4 卡片 + 模型分布表
//   - tasks:    agent 列表 + runs 时间线 + 事件展开
//
// 兼容性：保留所有 v0.2.0 路由（/api/state、/api/docs/{name}、/api/asks、
// /api/ask/{id}、/api/input、/api/config、/api/github、/api/events），
// 它们按后端当前选中项目工作。
function appData() {
  return {
    // ---- 全局 ----
    page: 'projects',
    agents: [
      { stage: 'listener', name: 'Listener 倾听者' },
      { stage: 'asker', name: 'Asker 询问者' },
      { stage: 'planner', name: 'Planner 规划者' },
      { stage: 'designer', name: 'Designer 设计者' },
      { stage: 'evaluator', name: 'Evaluator 评估者' },
      { stage: 'manager', name: 'Manager 管理者' },
      { stage: 'executor', name: 'Executor 执行者' },
      { stage: 'generator', name: 'Generator 生成者' },
      { stage: 'gittor', name: 'Gittor 提交者' },
    ],

    // ---- 项目 ----
    projects: [],
    currentID: null,
    currentProject: null,

    // ---- v0.2.0 流程/文档/问答/输入/GitHub ----
    state: { stage: '', agents: [] },
    docs: {},
    selectedDoc: 'desire',
    asks: [],
    answerInputs: {},
    inputRequest: '',
    githubForm: { remote: '', branch: '', token: '' },
    logs: [],

    // ---- 新建项目弹窗 ----
    showNewProject: false,
    newProject: { name: '', repo: '', branch: 'main' },
    ghRepos: [],
    ghReposError: '',

    // ---- 设置 ----
    roleModels: {},
    defaultModel: '',
    settingsSaved: false,

    // ---- 统计 ----
    stats: { summary: {}, usage: {}, concurrency: {} },
    autoRefreshStats: false,
    _statsTimer: null,

    // ---- 任务面板 ----
    taskAgent: 'listener',
    taskRuns: [],
    taskRunDetail: null,    // 可能是数组或 {events:[...]} 或 {run:{events:[...]}}
    selectedRunID: null,
    openedEvents: {},

    // ====== 生命周期 ======
    async init() {
      this.connectSSE();
      await this.loadProjects();
      await this.loadModels();
      // 定时轮询 v0.2.0 状态。
      setInterval(() => this.fetchState(), 2000);
      setInterval(() => this.fetchAsks(), 2000);
      setInterval(() => this.fetchDoc(this.selectedDoc), 3000);
      // 默认拉一次统计（失败不影响主流程）。
      this.loadStats().catch(() => {});
    },

    // ====== 页面切换 ======
    switchPage(p) {
      this.page = p;
      if (p === 'stats') {
        this.loadStats().catch(() => {});
      } else if (p === 'tasks') {
        if (this.currentID) this.loadRuns();
      }
    },

    // ====== 项目 ======
    async loadProjects() {
      try {
        const r = await fetch('/api/projects');
        if (r.ok) {
          const j = await r.json();
          this.projects = j.projects || [];
          const cur = j.current;
          if (cur) {
            this.currentID = cur;
            this.currentProject = this.projects.find(p => p.id === cur) || null;
          } else if (this.projects.length > 0 && !this.currentID) {
            // 首次进入若无选中项目，自动选第一个并通知后端。
            await this.selectProject(this.projects[0].id);
          }
          // 切换/加载项目后刷新 v0.2.0 视图。
          if (this.currentID) {
            this.fetchState();
            this.fetchDoc(this.selectedDoc);
            this.fetchAsks();
            this.fetchConfig();
          }
        }
      } catch (e) { /* 忽略瞬时网络错误 */ }
    },

    async selectProject(id) {
      if (!id) return;
      try {
        const r = await fetch('/api/projects/' + encodeURIComponent(id) + '/select', {
          method: 'POST',
        });
        if (r.ok) {
          this.currentID = id;
          this.currentProject = this.projects.find(p => p.id === id) || null;
          // 重置视图数据，再从后端按当前项目拉取。
          this.docs = {};
          this.asks = [];
          this.taskRuns = [];
          this.taskRunDetail = null;
          this.selectedRunID = null;
          this.fetchState();
          this.fetchDoc(this.selectedDoc);
          this.fetchAsks();
          this.fetchConfig();
        }
      } catch (e) { /* 忽略 */ }
    },

    async createProject() {
      const np = this.newProject;
      if (!np.name || !np.name.trim()) {
        alert('请填写项目名称');
        return;
      }
      try {
        const r = await fetch('/api/projects', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            name: np.name.trim(),
            repo: np.repo.trim(),
            branch: np.branch.trim() || 'main',
          }),
        });
        if (r.ok) {
          this.closeNewProject();
          await this.loadProjects();
          // 后端创建后已自动选中。
          if (this.currentID) {
            this.fetchState();
            this.fetchDoc(this.selectedDoc);
            this.fetchAsks();
          }
        } else {
          const j = await r.json().catch(() => ({}));
          alert('创建失败: ' + (j.error || r.status));
        }
      } catch (e) {
        alert('创建失败: ' + e.message);
      }
    },

    async deleteProject(id) {
      if (!confirm('确认删除该项目？该操作不可恢复。')) return;
      try {
        const r = await fetch('/api/projects/' + encodeURIComponent(id), { method: 'DELETE' });
        if (r.ok) {
          if (this.currentID === id) {
            this.currentID = null;
            this.currentProject = null;
            this.docs = {};
            this.asks = [];
            this.taskRuns = [];
            this.taskRunDetail = null;
            this.selectedRunID = null;
          }
          await this.loadProjects();
        } else {
          const j = await r.json().catch(() => ({}));
          alert('删除失败: ' + (j.error || r.status));
        }
      } catch (e) {
        alert('删除失败: ' + e.message);
      }
    },

    // ====== 新建项目弹窗 + gh repos ======
    async openNewProject() {
      this.newProject = { name: '', repo: '', branch: 'main' };
      this.showNewProject = true;
      await this.loadGhRepos();
    },

    closeNewProject() {
      this.showNewProject = false;
      this.ghReposError = '';
    },

    async loadGhRepos() {
      this.ghRepos = [];
      this.ghReposError = '';
      try {
        const r = await fetch('/api/gh/repos');
        if (r.ok) {
          const j = await r.json();
          this.ghRepos = j.repos || [];
        } else if (r.status === 401) {
          const j = await r.json().catch(() => ({}));
          this.ghReposError = j.login_hint || 'GitHub CLI 未登录，请运行：\n  gh auth login';
        } else {
          const j = await r.json().catch(() => ({}));
          this.ghReposError = j.error || ('拉取仓库失败: ' + r.status);
        }
      } catch (e) {
        this.ghReposError = '拉取仓库失败: ' + e.message;
      }
    },

    // ====== 启动编排 ======
    async startOrchestrator() {
      if (!this.currentID) return;
      try {
        const r = await fetch('/api/projects/' + encodeURIComponent(this.currentID) + '/start', {
          method: 'POST',
        });
        if (r.ok) {
          this.fetchState();
        } else if (r.status === 409) {
          alert('该项目已有运行中的编排器');
        } else {
          const j = await r.json().catch(() => ({}));
          alert('启动失败: ' + (j.error || r.status));
        }
      } catch (e) {
        alert('启动失败: ' + e.message);
      }
    },

    // ====== v0.2.0 输入/文档/Asker/GitHub ======
    async submitInput() {
      if (!this.inputRequest.trim()) return;
      if (!this.currentID) {
        alert('请先选择项目');
        return;
      }
      try {
        // 优先用项目级 input 路由；后端 /api/input 也兼容（按当前项目）。
        const r = await fetch('/api/projects/' + encodeURIComponent(this.currentID) + '/input', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ request: this.inputRequest }),
        });
        if (r.ok) {
          this.inputRequest = '';
          this.fetchDoc('desire');
        } else {
          const j = await r.json().catch(() => ({}));
          alert('提交失败: ' + (j.error || r.status));
        }
      } catch (e) { /* 忽略 */ }
    },

    selectDoc(d) {
      this.selectedDoc = d;
      this.fetchDoc(d);
    },

    async fetchState() {
      try {
        const r = await fetch('/api/state');
        if (r.ok) this.state = await r.json();
      } catch (e) { /* 忽略 */ }
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

    async submitAnswer(id) {
      const ans = this.answerInputs[id] || '';
      try {
        const r = await fetch('/api/ask/' + id, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ answer: ans }),
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
          body: JSON.stringify(this.githubForm),
        });
      } catch (e) { /* 忽略 */ }
    },

    // ====== 设置：角色模型 ======
    async loadModels() {
      try {
        const r = await fetch('/api/settings/models');
        if (r.ok) {
          const j = await r.json();
          this.roleModels = j.models || {};
          this.defaultModel = j.default || '';
          this.settingsSaved = false;
        }
      } catch (e) { /* 忽略 */ }
    },

    async saveModels() {
      try {
        const r = await fetch('/api/settings/models', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ models: this.roleModels }),
        });
        if (r.ok) {
          this.settingsSaved = true;
          setTimeout(() => { this.settingsSaved = false; }, 2000);
        } else {
          const j = await r.json().catch(() => ({}));
          alert('保存失败: ' + (j.error || r.status));
        }
      } catch (e) {
        alert('保存失败: ' + e.message);
      }
    },

    // ====== 统计 ======
    async loadStats() {
      try {
        const [s, u, c] = await Promise.allSettled([
          fetch('/api/stats/summary').then(r => r.ok ? r.json() : null),
          fetch('/api/stats/usage').then(r => r.ok ? r.json() : null),
          fetch('/api/stats/concurrency').then(r => r.ok ? r.json() : null),
        ]);
        if (s.value) this.stats.summary = s.value;
        if (u.value) this.stats.usage = u.value;
        if (c.value) this.stats.concurrency = c.value;
      } catch (e) { /* 忽略 */ }
    },

    toggleAutoRefreshStats() {
      if (this._statsTimer) {
        clearInterval(this._statsTimer);
        this._statsTimer = null;
      }
      if (this.autoRefreshStats) {
        this._statsTimer = setInterval(() => {
          if (this.page === 'stats') this.loadStats();
        }, 10000);
      }
    },

    formatUSD(v) {
      if (v == null || v === '') return '-';
      const n = Number(v);
      if (isNaN(n)) return v;
      return '$' + n.toFixed(4);
    },

    // ====== 任务面板 ======
    selectTaskAgent(stage) {
      this.taskAgent = stage;
      this.loadRuns();
    },

    async loadRuns() {
      if (!this.currentID) {
        this.taskRuns = [];
        return;
      }
      try {
        const r = await fetch('/api/projects/' + encodeURIComponent(this.currentID) + '/runs');
        if (r.ok) {
          const j = await r.json();
          let runs = j.runs || [];
          // 按 agent 过滤，时间倒序（后端已倒序，这里再保险一下）。
          runs = runs.filter(x => x.agent === this.taskAgent);
          runs.sort((a, b) => (b.created_at || 0) - (a.created_at || 0));
          this.taskRuns = runs;
        } else {
          this.taskRuns = [];
        }
      } catch (e) {
        this.taskRuns = [];
      }
    },

    async loadRunDetail(rid) {
      if (!this.currentID || !rid) return;
      this.selectedRunID = rid;
      this.taskRunDetail = null;
      this.openedEvents = {};
      try {
        const r = await fetch('/api/projects/' + encodeURIComponent(this.currentID) + '/runs/' + encodeURIComponent(rid));
        if (r.ok) {
          this.taskRunDetail = await r.json();
        }
      } catch (e) { /* 忽略 */ }
    },

    // 把 taskRunDetail 归一化为事件数组。
    runEvents() {
      if (!this.taskRunDetail) return [];
      if (Array.isArray(this.taskRunDetail)) return this.taskRunDetail;
      if (Array.isArray(this.taskRunDetail.events)) return this.taskRunDetail.events;
      if (this.taskRunDetail.run && Array.isArray(this.taskRunDetail.run.events)) return this.taskRunDetail.run.events;
      return [];
    },

    toggleEvent(i) {
      this.openedEvents[i] = !this.openedEvents[i];
    },

    // ====== SSE ======
    // 连接 /api/events，断开后自动重连。
    // agent_run_event: 推到 logs；若在任务面板且 run_id===selectedRunID，追加到 taskRunDetail.events。
    // 其他事件：推到 logs；agent_start/done/failed 刷新 state；ask_user 刷新 asks；doc_update 刷新当前文档。
    connectSSE() {
      const es = new EventSource('/api/events');
      es.onmessage = (e) => {
        try {
          const evt = JSON.parse(e.data);
          this.logs.push(evt);
          if (this.logs.length > 200) this.logs.shift();
          if (evt.type === 'agent_run_event') {
            // 任务面板实时追加事件（仅当用户正在看该 run）。
            if (this.page === 'tasks' && this.selectedRunID) {
              const data = evt.data || {};
              const rid = data.run_id;
              if (rid === this.selectedRunID) {
                const ev = {
                  type: data.event_type,
                  event_type: data.event_type,
                  content: data.content || '',
                  tool_name: data.tool_name || '',
                  tool_input: data.tool_input || '',
                  run_id: rid,
                };
                if (Array.isArray(this.taskRunDetail)) {
                  this.taskRunDetail.push(ev);
                } else if (this.taskRunDetail && Array.isArray(this.taskRunDetail.events)) {
                  this.taskRunDetail.events.push(ev);
                } else if (this.taskRunDetail && this.taskRunDetail.run && Array.isArray(this.taskRunDetail.run.events)) {
                  this.taskRunDetail.run.events.push(ev);
                } else {
                  // 还没拉到详情或详情为空数组，初始化为数组形式。
                  this.taskRunDetail = this.taskRunDetail || [];
                  if (Array.isArray(this.taskRunDetail)) {
                    this.taskRunDetail.push(ev);
                  }
                }
              }
            }
          } else {
            if (evt.type === 'agent_start' || evt.type === 'agent_done' || evt.type === 'agent_failed') {
              this.fetchState();
            }
            if (evt.type === 'ask_user') {
              this.fetchAsks();
            }
            if (evt.type === 'doc_update') {
              this.fetchDoc(this.selectedDoc);
            }
          }
        } catch (err) { /* 忽略解析错误 */ }
      };
      es.onerror = () => {
        es.close();
        setTimeout(() => this.connectSSE(), 3000);
      };
    },

    // ====== 样式/格式化辅助 ======
    projectStatusClass(s) {
      if (s === 'running') return 'bg-blue-100 text-blue-700';
      if (s === 'done') return 'bg-green-100 text-green-700';
      if (s === 'failed') return 'bg-red-100 text-red-700';
      return 'bg-gray-100 text-gray-600';
    },
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
    eventClass(ev) {
      const t = ev.event_type || ev.type;
      switch (t) {
        case 'thinking': return 'border-l-4 border-purple-400 bg-purple-50';
        case 'text': return 'border-l-4 border-gray-300';
        case 'tool_use': return 'border-l-4 border-blue-400 bg-blue-50';
        case 'tool_result': return 'border-l-4 border-green-400 bg-green-50';
        case 'result': return 'border-l-4 border-gray-700 bg-gray-50';
        case 'error': return 'border-l-4 border-red-500 bg-red-50';
        case 'system': return 'border-l-4 border-gray-200 bg-gray-50 text-gray-500';
        default: return 'border-l-4 border-gray-200';
      }
    },
    shortID(id) {
      if (!id) return '';
      return id.length > 12 ? id.slice(0, 12) + '…' : id;
    },
    formatTime(t) {
      if (!t) return '';
      try {
        const d = new Date(t);
        if (isNaN(d.getTime())) {
          // 可能是 unix 秒/毫秒
          const n = Number(t);
          if (!isNaN(n) && n > 0) {
            return new Date(n < 1e12 ? n * 1000 : n).toLocaleString();
          }
          return t;
        }
        return d.toLocaleString();
      } catch (e) { return t; }
    },
    formatData(d) {
      if (d == null) return '';
      if (typeof d === 'string') return d;
      try { return JSON.stringify(d); } catch (e) { return String(d); }
    },
  };
}
