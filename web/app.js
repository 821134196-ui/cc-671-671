const { createApp } = Vue;

createApp({
  data() {
    return {
      tab: 'board',
      show: null,
      board: null,
      actors: [],
      costumes: [],
      careList: [],
      issueList: [],
      checkSession: null,
      toasts: [],
      form: this.emptyForm(),
      previewConflicts: [],
      recast: { roleId: 0, newActorId: 0 },
      scan: { signTaskId: 0, signCode: '', returnTaskId: 0, returnCode: '' },
      newC: { code: '', name: '', changeMin: 5 },
      care: { costumeId: 0, type: 'laundry', note: '' },
      issue: { costumeId: 0, type: 'missing', note: '' },
    };
  },
  computed: {
    pendingTasks() { return (this.board ? this.board.tasks : []).filter(t => t.status === 'pending'); },
    signedTasks() { return (this.board ? this.board.tasks : []).filter(t => t.status === 'signed'); },
  },
  mounted() {
    this.init();
  },
  methods: {
    async api(method, url, body) {
      const opt = { method, headers: { 'Content-Type': 'application/json' } };
      if (body !== undefined) opt.body = JSON.stringify(body);
      const res = await fetch('/api' + url, opt);
      const j = await res.json().catch(() => ({}));
      if (!res.ok || j.ok === false) {
        const err = new Error(j.error || ('HTTP ' + res.status));
        err.conflicts = j.conflicts;
        throw err;
      }
      return j.data;
    },
    toast(msg, kind = 'ok') {
      this.toasts.push({ msg, kind });
      setTimeout(() => this.toasts.shift(), 4000);
    },
    async init() {
      const shows = await this.api('GET', '/shows');
      this.show = shows[0] || null;
      await Promise.all([this.loadActors(), this.loadCostumes(), this.loadBoard()]);
    },
    async loadActors() { this.actors = await this.api('GET', '/actors'); },
    async loadCostumes() { this.costumes = await this.api('GET', '/costumes'); },
    async loadBoard() {
      if (!this.show) return;
      this.board = await this.api('GET', `/shows/${this.show.id}/board`);
    },
    async loadCare() { this.careList = await this.api('GET', '/care'); },
    async loadIssues() {
      this.issueList = await this.api('GET', '/issues');
      this.issueList.forEach(i => { i._resolution = ''; });
    },
    emptyForm() {
      const today = new Date();
      const pad = n => String(n).padStart(2, '0');
      const d = `${today.getFullYear()}-${pad(today.getMonth() + 1)}-${pad(today.getDate())}`;
      return {
        costumeId: 0, roleId: 0, actorId: 0, sceneId: 0,
        onAt: `${d}T19:30`, offAt: `${d}T20:00`,
        changePoint: '侧台左', seq: 10,
      };
    },
    fmtHM(t) { return t ? new Date(t).toTimeString().slice(0, 5) : ''; },
    fmtDT(t) { return t ? new Date(t).toLocaleString('zh-CN', { hour12: false }) : ''; },
    costumeStatusText(s) {
      return { available: '可用', in_use: '使用中', laundering: '洗护中', repairing: '维修中', missing: '少件', damaged: '破损' }[s] || s;
    },
    taskStatusText(s) {
      return { pending: '待签收', signed: '已签收', returned: '已交回', cancelled: '已取消' }[s] || s;
    },
    checkStateText(s) {
      return { present: '在场', missing: '少件', damaged: '破损' }[s] || s;
    },
    conflictTypeText(t) {
      return {
        actor_overlap: '演员时间重叠',
        actor_change_tight: '演员来不及换装',
        costume_overlap: '戏服时间重叠',
        costume_turnover: '戏服转场/清洁不及',
        costume_blocked: '戏服未恢复可用',
      }[t] || t;
    },
    codeOf(id) { const c = this.costumes.find(x => x.id === id); return c ? c.code : '#' + id; },
    holderName(id) { const a = this.actors.find(x => x.id === id); return a ? a.name : ''; },
    quickCode(code) {
      if (this.scan.signTaskId) this.scan.signCode = code;
      else if (this.scan.returnTaskId) this.scan.returnCode = code;
      else this.scan.signCode = code;
    },

    formPayload() {
      return { ...this.form, showId: this.show.id };
    },
    async preCheck() {
      try {
        const r = await this.api('POST', '/tasks/check', this.formPayload());
        this.previewConflicts = r.conflicts;
        if (!r.conflicts.length) this.toast('预检通过：无冲突');
      } catch (e) { this.toast(e.message, 'err'); }
    },
    async saveTask(force) {
      try {
        await this.api('POST', '/tasks', { ...this.formPayload(), force });
        this.toast(force ? '已强制安排（冲突已记录）' : '换装任务已安排');
        this.previewConflicts = [];
        await this.loadBoard();
      } catch (e) {
        this.previewConflicts = e.conflicts || [];
        this.toast(e.message, 'err');
      }
    },
    async cancelTask(id) {
      await this.api('DELETE', `/tasks/${id}`);
      this.toast('任务已取消');
      this.loadBoard();
    },
    async doRecast() {
      if (!this.recast.roleId || !this.recast.newActorId) {
        this.toast('请选择角色和新演员', 'err'); return;
      }
      const r = await this.api('POST', '/recast', this.recast);
      this.toast(`临时换角完成：调整 ${r.changed} 个未签收任务${r.blocked.length ? `，${r.blocked.length} 项因冲突跳过` : ''}`);
      if (r.blocked.length) this.previewConflicts = r.blocked;
      await this.loadBoard();
    },

    async doSign() {
      try {
        await this.api('POST', '/scan/sign', { taskId: this.scan.signTaskId, actorId: 0, code: this.scan.signCode.trim() });
        this.toast('签收成功');
        this.scan.signCode = ''; this.scan.signTaskId = 0;
        await Promise.all([this.loadBoard(), this.loadCostumes()]);
      } catch (e) { this.toast(e.message, 'err'); }
    },
    async doReturn() {
      try {
        await this.api('POST', '/scan/return', { taskId: this.scan.returnTaskId, code: this.scan.returnCode.trim() });
        this.toast('交回成功');
        this.scan.returnCode = ''; this.scan.returnTaskId = 0;
        await Promise.all([this.loadBoard(), this.loadCostumes()]);
      } catch (e) { this.toast(e.message, 'err'); }
    },

    async addCostume() {
      if (!this.newC.code || !this.newC.name) { this.toast('编号和名称必填', 'err'); return; }
      try {
        await this.api('POST', '/costumes', {
          code: this.newC.code, name: this.newC.name,
          changeSeconds: (this.newC.changeMin || 0) * 60,
          status: 'available', location: '服装间',
        });
        this.toast('戏服已登记');
        this.newC = { code: '', name: '', changeMin: 5 };
        this.loadCostumes();
      } catch (e) { this.toast(e.message, 'err'); }
    },

    async startCare() {
      try {
        await this.api('POST', '/care', this.care);
        this.toast('已送出');
        this.care.note = '';
        await Promise.all([this.loadCare(), this.loadCostumes(), this.loadBoard()]);
      } catch (e) { this.toast(e.message, 'err'); }
    },
    async finishCare(id) {
      await this.api('POST', `/care/${id}/finish`);
      this.toast('洗护/维修完成，戏服恢复可用');
      await Promise.all([this.loadCare(), this.loadCostumes(), this.loadBoard()]);
    },

    async reportIssue() {
      try {
        await this.api('POST', '/issues', { ...this.issue, showId: this.show.id });
        this.toast('问题已登记，戏服已锁定');
        this.issue.note = '';
        await Promise.all([this.loadIssues(), this.loadCostumes(), this.loadBoard()]);
      } catch (e) { this.toast(e.message, 'err'); }
    },
    async resolveIssue(i) {
      if (!i._resolution) { this.toast('必须填写找回/修复经过', 'err'); return; }
      await this.api('POST', `/issues/${i.id}/resolve`, { resolution: i._resolution });
      this.toast('问题单已结单');
      await Promise.all([this.loadIssues(), this.loadCostumes(), this.loadBoard()]);
    },

    async startCheck() {
      this.checkSession = await this.api('POST', '/checks', { showId: this.show.id });
      this.checkSession.items.forEach(it => { it._note = it.note || ''; });
    },
    async recordCheck(it, state) {
      try {
        await this.api('POST', `/checks/${this.checkSession.session.id}/items`, {
          costumeId: it.costumeId, state, note: it._note,
        });
        this.toast(state === 'present' ? '已记为在场' : `已记为${this.checkStateText(state)}，问题单已生成`);
        this.checkSession = await this.api('GET', `/checks/${this.checkSession.session.id}`);
        this.checkSession.items.forEach(x => { x._note = x.note || ''; });
        await Promise.all([this.loadCostumes(), this.loadIssues(), this.loadBoard()]);
      } catch (e) { this.toast(e.message, 'err'); }
    },
  },
}).mount('#app');
