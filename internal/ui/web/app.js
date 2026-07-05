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
const I18N = {
  zh: {
    'app.title': 'zzauto',
    'nav.projects': '项目',
    'nav.settings': '设置',
    'nav.stats': '统计',
    'nav.tasks': '任务面板',
    'project.current': '当前项目',
    'project.none': '未选择',
    'project.stage': '阶段',
    'projects.title': '项目列表',
    'projects.new': '新建项目',
    'projects.col.name': '名称',
    'projects.col.repo': '仓库',
    'projects.col.status': '状态',
    'projects.col.created': '创建',
    'projects.col.actions': '操作',
    'projects.empty': '暂无项目',
    'projects.createFirst': '新建第一个项目',
    'projects.delete': '删除',
    'flow.title': '流程步骤',
    'flow.selectFirst': '请先选择或创建项目',
    'flow.startOrch': '启动编排',
    'flow.pause': '暂停',
    'flow.resume': '继续',
    'flow.stop': '终止',
    'flow.paused': '已暂停',
    'flow.running': '运行中',
    'flow.idle': '未启动',
    'doc.viewer': '文档查看器',
    'doc.empty': '暂无内容',
    'doc.selectProject': '请先选择项目',
    'doc.stage': '阶段',
    'doc.status': '状态',
    'doc.updated': '更新',
    'input.title': '提交需求',
    'input.placeholder': '描述你想要构建的应用...',
    'input.submit': '提交',
    'ask.title': '待回答问题',
    'ask.placeholder': '输入回答...',
    'ask.answer': '回答',
    'ask.empty': '暂无待答问题',
    'req.title': '追加新需求（异步）',
    'req.placeholder': '随时补充新需求，Mixor 会判定是否冲突，未冲突则合并到 spec，冲突则回退到 Analyst 重跑...',
    'req.submit': '追加',
    'req.queue': '需求队列快照',
    'req.empty': '（空）',
    'files.title': '文件浏览',
    'files.empty': '该目录无文件',
    'files.up': '上级',
    'files.refresh': '刷新',
    'files.preview': '预览',
    'files.back': '返回列表',
    'pm.title': '项目级模型覆盖',
    'pm.hint': '留空继承全局；修改后下次该角色被调用时生效（无需重启）',
    'pm.inherit': '继承全局',
    'pm.saved': '已保存 ✓',
    'pm.save': '保存',
    'np.localDir': '本地目录（可选）',
    'np.localDirPlaceholder': '/path/to/local/dir',
    'np.localDirHint': '非空时 workspace 直接指向该目录，便于查看/编辑本地代码；删除项目不会删除该目录',
    'github.title': 'GitHub 配置覆盖（可选）',
    'github.hint': '默认由 gh CLI 管理 GitHub 鉴权；以下字段仅在需要覆盖远端或分支时填写。',
    'github.remote': 'Remote',
    'github.branch': 'Branch',
    'github.token': 'Token（可选 override）',
    'github.save': '保存',
    'settings.title': '角色模型设置',
    'settings.reload': '重新加载',
    'settings.hint': '为每个 agent 角色配置独立模型（覆盖默认模型）。留空则使用 aiclibridge 默认模型',
    'settings.default': '默认（使用 aiclibridge 默认模型）',
    'settings.saved': '已保存 ✓',
    'settings.save': '保存',
    'stats.title': '统计面板',
    'stats.refresh': '刷新',
    'stats.autoRefresh': '自动刷新（10s）',
    'stats.totalReq': '总请求数',
    'stats.totalToken': '总 Token',
    'stats.totalUSD': '总 USD',
    'stats.concurrency': '并发',
    'stats.activeQueuedMax': 'active / queued / max',
    'stats.modelDist': '模型分布',
    'stats.models': '个模型',
    'stats.col.model': '模型',
    'stats.col.prompt': 'prompt_tokens',
    'stats.col.completion': 'completion_tokens',
    'stats.col.total': 'total_tokens',
    'stats.col.requests': 'requests',
    'stats.col.usd': 'USD',
    'stats.noData': '暂无数据',
    'tasks.title': '任务面板',
    'tasks.selectProject': '请先选择项目',
    'tasks.runs': 'Runs',
    'tasks.refresh': '刷新',
    'tasks.noRun': '暂无 run',
    'tasks.timeline': '事件时间线',
    'tasks.selectRun': '选择左侧 run 查看事件',
    'tasks.noEvents': '暂无事件',
    'tasks.expand': '展开',
    'tasks.collapse': '收起',
    'logs.title': '实时日志',
    'logs.count': '条',
    'logs.clear': '清空',
    'logs.waiting': '等待事件...',
    'np.title': '新建项目',
    'np.name': '名称 *',
    'np.namePlaceholder': '项目名称',
    'np.repo': '仓库',
    'np.repoPlaceholder': 'owner/name',
    'np.repoManual': '（手动输入或选择）',
    'np.branch': '分支',
    'np.branchPlaceholder': 'main',
    'np.cancel': '取消',
    'np.create': '创建',
    'np.ghNotLoggedIn': 'GitHub CLI 未登录，请运行：\n  gh auth login',
    'st.pending': '待执行',
    'st.running': '运行中',
    'st.done': '完成',
    'st.failed': '失败',
    'st.paused': '已暂停',
    'msg.confirmDelete': '确认删除该项目？该操作不可恢复。',
    'msg.nameRequired': '请填写项目名称',
    'msg.createFailed': '创建失败: ',
    'msg.deleteFailed': '删除失败: ',
    'msg.startFailed': '启动失败: ',
    'msg.orchRunning': '该项目已有运行中的编排器',
    'msg.selectProject': '请先选择项目',
    'msg.submitFailed': '提交失败: ',
    'msg.saveFailed': '保存失败: ',
    'msg.pauseFailed': '暂停失败: ',
    'msg.stopFailed': '停止失败: ',
    'msg.resumeFailed': '继续失败: ',
    'msg.reqFailed': '追加需求失败: ',
    'msg.notRunning': '项目未运行',
    'msg.confirmStop': '确认终止当前任务？已完成的阶段会保留，但运行中的编排器将立即停止。',
    'lang.label': '语言',
    'theme.label': '主题',
    'theme.toggleLight': '切换到浅色',
    'theme.toggleDark': '切换到深色',
  },
  en: {
    'app.title': 'zzauto',
    'nav.projects': 'Projects',
    'nav.settings': 'Settings',
    'nav.stats': 'Stats',
    'nav.tasks': 'Tasks',
    'project.current': 'Current',
    'project.none': 'None',
    'project.stage': 'Stage',
    'projects.title': 'Projects',
    'projects.new': 'New Project',
    'projects.col.name': 'Name',
    'projects.col.repo': 'Repo',
    'projects.col.status': 'Status',
    'projects.col.created': 'Created',
    'projects.col.actions': 'Actions',
    'projects.empty': 'No projects',
    'projects.createFirst': 'Create your first project',
    'projects.delete': 'Delete',
    'flow.title': 'Workflow Steps',
    'flow.selectFirst': 'Please select or create a project first',
    'flow.startOrch': 'Start Orchestration',
    'flow.pause': 'Pause',
    'flow.resume': 'Resume',
    'flow.stop': 'Stop',
    'flow.paused': 'Paused',
    'flow.running': 'Running',
    'flow.idle': 'Idle',
    'doc.viewer': 'Document Viewer',
    'doc.empty': 'No content',
    'doc.selectProject': 'Please select a project first',
    'doc.stage': 'Stage',
    'doc.status': 'Status',
    'doc.updated': 'Updated',
    'input.title': 'Submit Request',
    'input.placeholder': 'Describe the app you want to build...',
    'input.submit': 'Submit',
    'ask.title': 'Pending Questions',
    'ask.placeholder': 'Enter answer...',
    'ask.answer': 'Answer',
    'ask.empty': 'No pending questions',
    'req.title': 'Append New Requirement (async)',
    'req.placeholder': 'Add new requirements anytime; Mixor decides: no conflict -> merge into spec, conflict -> rerun from Analyst',
    'req.submit': 'Append',
    'req.queue': 'Requirements Queue Snapshot',
    'req.empty': '(empty)',
    'files.title': 'File Browser',
    'files.empty': 'No files in this directory',
    'files.up': 'Up',
    'files.refresh': 'Refresh',
    'files.preview': 'Preview',
    'files.back': 'Back to list',
    'pm.title': 'Project-level Model Override',
    'pm.hint': 'Empty = inherit global; changes take effect on next agent invocation (no restart needed)',
    'pm.inherit': 'Inherit global',
    'pm.saved': 'Saved ✓',
    'pm.save': 'Save',
    'np.localDir': 'Local Dir (optional)',
    'np.localDirPlaceholder': '/path/to/local/dir',
    'np.localDirHint': 'When set, workspace points to this dir for easy view/edit; deleting the project will NOT delete this dir',
    'github.title': 'GitHub Config Override (optional)',
    'github.hint': 'GitHub auth is managed by gh CLI by default; fill these fields only when overriding remote or branch.',
    'github.remote': 'Remote',
    'github.branch': 'Branch',
    'github.token': 'Token (optional override)',
    'github.save': 'Save',
    'settings.title': 'Role Model Settings',
    'settings.reload': 'Reload',
    'settings.hint': 'Configure an independent model for each agent role (overrides default). Leave empty to use the aiclibridge default model',
    'settings.default': 'Default (use aiclibridge default)',
    'settings.saved': 'Saved ✓',
    'settings.save': 'Save',
    'stats.title': 'Statistics',
    'stats.refresh': 'Refresh',
    'stats.autoRefresh': 'Auto refresh (10s)',
    'stats.totalReq': 'Total Requests',
    'stats.totalToken': 'Total Tokens',
    'stats.totalUSD': 'Total USD',
    'stats.concurrency': 'Concurrency',
    'stats.activeQueuedMax': 'active / queued / max',
    'stats.modelDist': 'Model Distribution',
    'stats.models': 'models',
    'stats.col.model': 'Model',
    'stats.col.prompt': 'prompt_tokens',
    'stats.col.completion': 'completion_tokens',
    'stats.col.total': 'total_tokens',
    'stats.col.requests': 'requests',
    'stats.col.usd': 'USD',
    'stats.noData': 'No data',
    'tasks.title': 'Task Panel',
    'tasks.selectProject': 'Please select a project first',
    'tasks.runs': 'Runs',
    'tasks.refresh': 'Refresh',
    'tasks.noRun': 'No runs',
    'tasks.timeline': 'Event Timeline',
    'tasks.selectRun': 'Select a run on the left to view events',
    'tasks.noEvents': 'No events',
    'tasks.expand': 'Expand',
    'tasks.collapse': 'Collapse',
    'logs.title': 'Live Logs',
    'logs.count': 'entries',
    'logs.clear': 'Clear',
    'logs.waiting': 'Waiting for events...',
    'np.title': 'New Project',
    'np.name': 'Name *',
    'np.namePlaceholder': 'Project name',
    'np.repo': 'Repository',
    'np.repoPlaceholder': 'owner/name',
    'np.repoManual': '(manual input or select)',
    'np.branch': 'Branch',
    'np.branchPlaceholder': 'main',
    'np.cancel': 'Cancel',
    'np.create': 'Create',
    'np.ghNotLoggedIn': 'GitHub CLI not logged in, please run:\n  gh auth login',
    'st.pending': 'Pending',
    'st.running': 'Running',
    'st.done': 'Done',
    'st.failed': 'Failed',
    'st.paused': 'Paused',
    'msg.confirmDelete': 'Are you sure you want to delete this project? This action cannot be undone.',
    'msg.nameRequired': 'Please enter a project name',
    'msg.createFailed': 'Create failed: ',
    'msg.deleteFailed': 'Delete failed: ',
    'msg.startFailed': 'Start failed: ',
    'msg.orchRunning': 'An orchestrator is already running for this project',
    'msg.selectProject': 'Please select a project first',
    'msg.submitFailed': 'Submit failed: ',
    'msg.saveFailed': 'Save failed: ',
    'msg.pauseFailed': 'Pause failed: ',
    'msg.stopFailed': 'Stop failed: ',
    'msg.resumeFailed': 'Resume failed: ',
    'msg.reqFailed': 'Append requirement failed: ',
    'msg.notRunning': 'Project not running',
    'msg.confirmStop': 'Stop the current task? Completed stages are preserved, but the running orchestrator will be terminated immediately.',
    'lang.label': 'Language',
    'theme.label': 'Theme',
    'theme.toggleLight': 'Switch to light',
    'theme.toggleDark': 'Switch to dark',
  }
};

function appData() {
  return {
    // ---- i18n & 主题 ----
    lang: localStorage.getItem('zzauto-lang') || 'zh',
    theme: localStorage.getItem('zzauto-theme') || 'light',

    t(key) {
      return ((I18N[this.lang] || I18N.zh)[key]) || key;
    },

    setLang(l) {
      this.lang = l;
      localStorage.setItem('zzauto-lang', l);
    },

    toggleTheme() {
      this.theme = this.theme === 'light' ? 'dark' : 'light';
      localStorage.setItem('zzauto-theme', this.theme);
      this.applyTheme();
    },

    applyTheme() {
      if (this.theme === 'dark') {
        document.documentElement.classList.add('dark');
      } else {
        document.documentElement.classList.remove('dark');
      }
    },

    // ---- 全局 ----
    page: 'projects',
    agents: [
      { stage: 'analyst',   name: 'Analyst 分析者' },
      { stage: 'architect', name: 'Architect 架构师' },
      { stage: 'planner',   name: 'Planner 规划者' },
      { stage: 'coder',     name: 'Coder 编写者' },
      { stage: 'reviewer',  name: 'Reviewer 审查者' },
      { stage: 'mixor',     name: 'Mixor 融合者' },
    ],

    // ---- 项目 ----
    projects: [],
    currentID: null,
    currentProject: null,

    // ---- 流程/文档/问答/输入/GitHub ----
    state: { stage: '', agents: [] },
    docs: {},
    selectedDoc: 'input',
    asks: [],
    answerInputs: {},
    inputRequest: '',
    githubForm: { remote: '', branch: '', token: '' },
    logs: [],

    // ---- 新建项目弹窗 ----
    showNewProject: false,
    newProject: { name: '', repo: '', branch: 'main', localDir: '' },
    ghRepos: [],
    ghReposError: '',

    // ---- 设置（全局） ----
    roleModels: {},
    defaultModel: '',
    settingsSaved: false,
    availableModels: [],  // 来自 /api/aicli/models

    // ---- 项目控制（暂停/停止/继续）----
    // orchState 由 SSE 与定时轮询刷新：running / paused / stopped
    orchState: { running: false, paused: false },

    // ---- 文件浏览 ----
    filesPath: '.',     // 当前浏览的相对路径
    filesEntries: [],   // 当前路径下的文件/目录列表
    filePreview: '',    // 当前预览的文件内容
    filePreviewName: '',

    // ---- 异步需求队列 ----
    newRequirement: '',
    reqQueue: '',       // requirements_queue.md 内容快照

    // ---- 项目级模型 ----
    projectModels: {},      // 项目级覆盖
    projectModelsGlobal: {},// 全局配置（用于参考）
    projectModelsSaved: false,

    // ---- 统计 ----
    stats: { summary: {}, usage: {}, concurrency: {} },
    autoRefreshStats: false,
    _statsTimer: null,

    // ---- 任务面板 ----
    taskAgent: 'analyst',
    taskRuns: [],
    taskRunDetail: null,    // 可能是数组或 {events:[...]} 或 {run:{events:[...]}}
    selectedRunID: null,
    openedEvents: {},

    // ====== 生命周期 ======
    async init() {
      this.applyTheme();
      this.connectSSE();
      await this.loadProjects();
      await this.loadModels();
      await this.loadAvailableModels();
      // 定时轮询状态。
      setInterval(() => this.fetchState(), 2000);
      setInterval(() => this.fetchAsks(), 2000);
      setInterval(() => this.fetchDoc(this.selectedDoc), 3000);
      // 定时刷新项目状态（用于检测 running/paused 切换）。
      setInterval(() => {
        this.loadProjects();
      }, 5000);
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
          // 切换/加载项目后刷新视图。
          if (this.currentID) {
            this.fetchState();
            this.fetchDoc(this.selectedDoc);
            this.fetchAsks();
            this.fetchConfig();
            this.refreshOrchState();
            this.loadFiles();
            this.loadReqQueue();
            this.loadProjectModels();
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
          this.filesPath = '.';
          this.filesEntries = [];
          this.filePreview = '';
          this.filePreviewName = '';
          this.newRequirement = '';
          this.reqQueue = '';
          this.fetchState();
          this.fetchDoc(this.selectedDoc);
          this.fetchAsks();
          this.fetchConfig();
          this.refreshOrchState();
          this.loadFiles();
          this.loadReqQueue();
          this.loadProjectModels();
        }
      } catch (e) { /* 忽略 */ }
    },

    async createProject() {
      const np = this.newProject;
      if (!np.name || !np.name.trim()) {
        alert(this.t('msg.nameRequired'));
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
            local_dir: np.localDir.trim(),
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
            this.loadFiles();
          }
        } else {
          const j = await r.json().catch(() => ({}));
          alert(this.t('msg.createFailed') + (j.error || r.status));
        }
      } catch (e) {
        alert(this.t('msg.createFailed') + e.message);
      }
    },

    async deleteProject(id) {
      if (!confirm(this.t('msg.confirmDelete'))) return;
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
          alert(this.t('msg.deleteFailed') + (j.error || r.status));
        }
      } catch (e) {
        alert(this.t('msg.deleteFailed') + e.message);
      }
    },

    // ====== 新建项目弹窗 + gh repos ======
    async openNewProject() {
      this.newProject = { name: '', repo: '', branch: 'main', localDir: '' };
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
          this.ghReposError = j.login_hint || this.t('np.ghNotLoggedIn');
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
          this.refreshOrchState();
        } else if (r.status === 409) {
          alert(this.t('msg.orchRunning'));
        } else {
          const j = await r.json().catch(() => ({}));
          alert(this.t('msg.startFailed') + (j.error || r.status));
        }
      } catch (e) {
        alert(this.t('msg.startFailed') + e.message);
      }
    },

    // ====== 控制编排：暂停/停止/继续 ======
    // orchState 通过 currentProject.status 与 SSE 事件推断：
    //   running / paused / failed / pending / done
    refreshOrchState() {
      const s = this.currentProject?.status;
      this.orchState.running = (s === 'running' || s === 'paused');
      this.orchState.paused = (s === 'paused');
    },

    orchStatusBadge() {
      const s = this.currentProject?.status;
      if (s === 'running') return this.t('flow.running');
      if (s === 'paused') return this.t('flow.paused');
      return this.t('flow.idle');
    },

    async pauseOrchestrator() {
      if (!this.currentID) return;
      try {
        const r = await fetch('/api/projects/' + encodeURIComponent(this.currentID) + '/pause', { method: 'POST' });
        if (r.ok) {
          await this.loadProjects();
        } else {
          const j = await r.json().catch(() => ({}));
          alert(this.t('msg.pauseFailed') + (j.error || r.status));
        }
      } catch (e) {
        alert(this.t('msg.pauseFailed') + e.message);
      }
    },

    async resumeOrchestrator() {
      if (!this.currentID) return;
      try {
        const r = await fetch('/api/projects/' + encodeURIComponent(this.currentID) + '/resume', { method: 'POST' });
        if (r.ok) {
          await this.loadProjects();
        } else {
          const j = await r.json().catch(() => ({}));
          alert(this.t('msg.resumeFailed') + (j.error || r.status));
        }
      } catch (e) {
        alert(this.t('msg.resumeFailed') + e.message);
      }
    },

    async stopOrchestrator() {
      if (!this.currentID) return;
      if (!confirm(this.t('msg.confirmStop'))) return;
      try {
        const r = await fetch('/api/projects/' + encodeURIComponent(this.currentID) + '/stop', { method: 'POST' });
        if (r.ok) {
          await this.loadProjects();
        } else {
          const j = await r.json().catch(() => ({}));
          alert(this.t('msg.stopFailed') + (j.error || r.status));
        }
      } catch (e) {
        alert(this.t('msg.stopFailed') + e.message);
      }
    },

    // ====== 文件浏览 ======
    async loadFiles(path) {
      if (!this.currentID) {
        this.filesEntries = [];
        return;
      }
      if (path !== undefined) this.filesPath = path;
      try {
        const url = '/api/projects/' + encodeURIComponent(this.currentID) +
                    '/files?path=' + encodeURIComponent(this.filesPath);
        const r = await fetch(url);
        if (r.ok) {
          const j = await r.json();
          this.filesEntries = j.files || [];
        } else {
          this.filesEntries = [];
        }
      } catch (e) {
        this.filesEntries = [];
      }
    },

    filesUpPath() {
      if (this.filesPath === '.' || this.filesPath === '') return '.';
      const parts = this.filesPath.split('/');
      parts.pop();
      return parts.length === 0 ? '.' : parts.join('/');
    },

    openDir(name) {
      const next = this.filesPath === '.' ? name : this.filesPath + '/' + name;
      this.loadFiles(next);
    },

    async previewFile(name) {
      if (!this.currentID) return;
      const rel = this.filesPath === '.' ? name : this.filesPath + '/' + name;
      try {
        const url = '/api/projects/' + encodeURIComponent(this.currentID) +
                    '/file?path=' + encodeURIComponent(rel);
        const r = await fetch(url);
        if (r.ok) {
          const j = await r.json();
          this.filePreviewName = rel;
          const trunc = j.truncated ? '\n\n…（文件已截断，仅显示前 256KB）' : '';
          this.filePreview = (j.content || '') + trunc;
        } else {
          const j = await r.json().catch(() => ({}));
          this.filePreviewName = rel;
          this.filePreview = '预览失败: ' + (j.error || r.status);
        }
      } catch (e) {
        this.filePreview = '预览失败: ' + e.message;
      }
    },

    closeFilePreview() {
      this.filePreview = '';
      this.filePreviewName = '';
    },

    // ====== 异步需求队列 ======
    async loadReqQueue() {
      if (!this.currentID) {
        this.reqQueue = '';
        return;
      }
      try {
        const r = await fetch('/api/docs/queue');
        if (r.ok) {
          const j = await r.json();
          this.reqQueue = j.body || j.raw || '';
        } else {
          this.reqQueue = '';
        }
      } catch (e) {
        this.reqQueue = '';
      }
    },

    async submitRequirement() {
      if (!this.currentID) {
        alert(this.t('msg.selectProject'));
        return;
      }
      if (!this.newRequirement.trim()) return;
      try {
        const r = await fetch('/api/projects/' + encodeURIComponent(this.currentID) + '/requirement', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ request: this.newRequirement }),
        });
        if (r.ok) {
          this.newRequirement = '';
          this.loadReqQueue();
        } else {
          const j = await r.json().catch(() => ({}));
          alert(this.t('msg.reqFailed') + (j.error || r.status));
        }
      } catch (e) {
        alert(this.t('msg.reqFailed') + e.message);
      }
    },

    // ====== 项目级模型 ======
    async loadProjectModels() {
      if (!this.currentID) {
        this.projectModels = {};
        this.projectModelsGlobal = {};
        return;
      }
      try {
        const r = await fetch('/api/projects/' + encodeURIComponent(this.currentID) + '/models');
        if (r.ok) {
          const j = await r.json();
          // 深拷贝避免双向绑定改到全局视图
          this.projectModels = { ...(j.models || {}) };
          this.projectModelsGlobal = { ...(j.global || {}) };
          this.projectModelsSaved = false;
        }
      } catch (e) { /* 忽略 */ }
    },

    async saveProjectModels() {
      if (!this.currentID) return;
      try {
        const r = await fetch('/api/projects/' + encodeURIComponent(this.currentID) + '/models', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ models: this.projectModels }),
        });
        if (r.ok) {
          this.projectModelsSaved = true;
          setTimeout(() => { this.projectModelsSaved = false; }, 2000);
        } else {
          const j = await r.json().catch(() => ({}));
          alert(this.t('msg.saveFailed') + (j.error || r.status));
        }
      } catch (e) {
        alert(this.t('msg.saveFailed') + e.message);
      }
    },

    // 返回某 stage 的全局模型（用于显示"继承全局"提示）
    inheritedModel(stage) {
      return this.projectModelsGlobal[stage] || this.defaultModel || 'claude/anthropic/claude-sonnet-4.5';
    },

    // ====== v0.2.0 输入/文档/Asker/GitHub ======
    async submitInput() {
      if (!this.inputRequest.trim()) return;
      if (!this.currentID) {
        alert(this.t('msg.selectProject'));
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
          this.fetchDoc('input');
        } else {
          const j = await r.json().catch(() => ({}));
          alert(this.t('msg.submitFailed') + (j.error || r.status));
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

    async loadAvailableModels() {
      try {
        const resp = await fetch('/api/aicli/models');
        if (!resp.ok) return;
        const data = await resp.json();
        this.availableModels = (data.data || []).map(m => m.id);
      } catch (e) {
        // 失败时退化为空数组，input 仍可手输
        this.availableModels = [];
      }
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
          alert(this.t('msg.saveFailed') + (j.error || r.status));
        }
      } catch (e) {
        alert(this.t('msg.saveFailed') + e.message);
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
            // 编排器暂停/恢复/停止事件：刷新项目状态
            if (evt.type === 'orch_paused' || evt.type === 'orch_resumed' || evt.type === 'orch_stopped') {
              this.loadProjects();
            }
            // Mixor 处理需求队列后：刷新队列与文档视图
            if (evt.type === 'agent_done' && evt.agent === 'mixor') {
              this.loadReqQueue();
              this.fetchDoc('spec');
              this.fetchDoc('progress');
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
      if (s === 'running') return 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300';
      if (s === 'paused') return 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/40 dark:text-yellow-300';
      if (s === 'done') return 'bg-green-100 text-green-700 dark:bg-green-900/40 dark:text-green-300';
      if (s === 'failed') return 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300';
      return 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300';
    },
    stageBadgeClass(stage) {
      if (!stage) return 'bg-gray-200 text-gray-600 dark:bg-gray-700 dark:text-gray-300';
      return 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300';
    },
    agentRowClass(a) {
      if (a.status === 'running') return 'bg-blue-50 dark:bg-blue-900/20';
      if (a.status === 'done') return 'bg-green-50 dark:bg-green-900/20';
      if (a.status === 'failed') return 'bg-red-50 dark:bg-red-900/20';
      return '';
    },
    agentDotClass(a) {
      if (a.status === 'running') return 'bg-blue-600 text-white animate-pulse';
      if (a.status === 'done') return 'bg-green-600 text-white';
      if (a.status === 'failed') return 'bg-red-600 text-white';
      return 'bg-gray-300 text-gray-600 dark:bg-gray-600 dark:text-gray-200';
    },
    statusLabel(s) {
      const map = {
        pending: this.t('st.pending'),
        running: this.t('st.running'),
        done: this.t('st.done'),
        failed: this.t('st.failed'),
        paused: this.t('st.paused'),
      };
      return map[s] || s;
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
