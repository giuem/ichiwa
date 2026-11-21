(function () {
  "use strict";

  var REPO = "giuem/ichiwa";

  /* ---------- Download menu ---------- */

  var dlButton = document.querySelector(".download .button");
  var dlMenu = document.getElementById("download-menu");
  var dlItems = Array.prototype.slice.call(dlMenu.querySelectorAll("a"));

  function setMenu(open, focusFirst) {
    dlMenu.hidden = !open;
    dlButton.setAttribute("aria-expanded", open ? "true" : "false");
    if (open && focusFirst) dlItems[0].focus();
  }
  dlButton.addEventListener("click", function () { setMenu(dlMenu.hidden, false); });
  dlButton.addEventListener("keydown", function (e) {
    if (e.key === "ArrowDown") { setMenu(true, true); e.preventDefault(); }
  });
  dlMenu.addEventListener("keydown", function (e) {
    var idx = dlItems.indexOf(document.activeElement);
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      var d = e.key === "ArrowDown" ? 1 : -1;
      dlItems[(idx + d + dlItems.length) % dlItems.length].focus();
      e.preventDefault();
    } else if (e.key === "Escape") {
      setMenu(false);
      dlButton.focus();
    } else if (e.key === "Tab") {
      setMenu(false);
    }
  });
  dlItems.forEach(function (a) { a.addEventListener("click", function () { setMenu(false); }); });
  document.addEventListener("click", function (e) {
    if (!dlMenu.hidden && !e.target.closest(".download")) setMenu(false);
  });

  // Point the .dmg items at the latest release's files; without the API they
  // keep linking to the release page.
  fetch("https://api.github.com/repos/" + REPO + "/releases/latest", {
    headers: { Accept: "application/vnd.github+json" }
  })
    .then(function (r) { return r.ok ? r.json() : null; })
    .then(function (release) {
      if (!release) return;
      var assets = release.assets || [];
      dlMenu.querySelectorAll("[data-asset]").forEach(function (link) {
        var suffix = link.getAttribute("data-asset");
        for (var j = 0; j < assets.length; j++) {
          if (assets[j].name.slice(-suffix.length) === suffix) {
            link.href = assets[j].browser_download_url;
            break;
          }
        }
      });
      if (release.tag_name) {
        document.getElementById("version").textContent = " · " + release.tag_name.replace(/^v/, "");
      }
    })
    .catch(function () {});

  /* ---------- Copy ---------- */

  function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) return navigator.clipboard.writeText(text);
    return new Promise(function (resolve, reject) {
      var ta = document.createElement("textarea");
      ta.value = text;
      ta.setAttribute("readonly", "");
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      var ok = false;
      try { ok = document.execCommand("copy"); } catch (err) {}
      document.body.removeChild(ta);
      ok ? resolve() : reject();
    });
  }
  document.querySelectorAll(".copy").forEach(function (btn) {
    btn.addEventListener("click", function () {
      copyText(btn.parentNode.querySelector("code").textContent).then(function () {
        btn.textContent = "已复制";
        setTimeout(function () { btn.textContent = "复制"; }, 1600);
      }, function () {
        btn.textContent = "请手动复制";
      });
    });
  });

  /* ---------- Player ---------- */

  var app = document.getElementById("app");
  if (!app) return;

  function $(id) { return document.getElementById(id); }
  var stage = $("stage"), layer = $("dm-layer"), scene = $("scene"), subtitle = $("subtitle");
  var seek = $("seek"), seekFill = $("seek-fill"), seekKnob = $("seek-knob"), seekTip = $("seek-tip");
  var playBtn = $("play"), toastEl = $("toast"), boostEl = $("boost");

  // The same steps and defaults as the app (internal/app/danmaku.go).
  var SPEEDS = [0.75, 1, 1.25, 1.5, 2];
  var DM_SPEEDS = [{ label: "很慢", flight: 13 }, { label: "较慢", flight: 10 }, { label: "适中", flight: 8 }, { label: "较快", flight: 6 }, { label: "很快", flight: 4.5 }];
  var DM_AREAS = [{ label: "2行", lanes: 2 }, { label: "1/4屏", share: 0.25 }, { label: "半屏", share: 0.5 }, { label: "3/4屏", share: 0.75 }, { label: "满屏", share: 1 }];
  var DM_SIZES = [{ label: "很小", size: 13 }, { label: "较小", size: 15 }, { label: "适中", size: 17 }, { label: "较大", size: 20 }, { label: "很大", size: 24 }];
  var DM_GAP = 28, DM_TOP = 56, DM_BOTTOM = 96, DM_MERGE = 8;
  var CHROME_LINGER = 3000, KEY_LINGER = 1500, CLICK_DELAY = 220, BOOST_DELAY = 350;

  var EPISODES = 80, PER_RANGE = 50, DURATION = 125;
  var LOCKED = { 77: true, 78: true, 79: true };

  var st = {
    ep: 12,
    pos: 72,
    playing: false,
    userPaused: false,
    rate: 1,
    boost: false,
    danmaku: true,
    muted: false,
    opacity: 1, dmSpeed: 2, dmArea: 2, dmSize: 2,
    range: 0,
    progress: {} // by episode: fraction watched; 1 is done
  };
  for (var e0 = 0; e0 < 12; e0++) st.progress[e0] = 1;
  st.progress[13] = 0.3;

  function fmt(s) {
    s = Math.max(0, Math.floor(s));
    var m = Math.floor(s / 60), r = s % 60;
    return (m < 10 ? "0" : "") + m + ":" + (r < 10 ? "0" : "") + r;
  }

  // A seeded random, so an episode's comments are the same each time.
  function rng(seed) {
    return function () {
      seed |= 0; seed = seed + 0x6D2B79F5 | 0;
      var t = Math.imul(seed ^ seed >>> 15, 1 | seed);
      t = t + Math.imul(t ^ t >>> 7, 61 | t) ^ t;
      return ((t ^ t >>> 14) >>> 0) / 4294967296;
    };
  }

  // The service's emoji codes, as the app shows them (internal/catalog/danmaku.go).
  var EMOJI = {
    "[笑哭]": "😂", "[捂脸]": "🤦", "[赞]": "👍", "[爱慕]": "🥰", "[吃瓜]": "🍉", "[偷笑]": "🤭", "[爽]": "😎",
    "[奸笑]": "😏", "[送花]": "💐", "[爱心]": "❤️", "[大笑]": "😆", "[哭]": "😭", "[鼓掌]": "👏", "[惊呆]": "😱",
    "[流泪]": "😭", "[害羞]": "😊", "[色]": "😍", "[强]": "💪", "[比心]": "🫶", "[火]": "🔥", "[心碎]": "💔", "[石化]": "🗿"
  };
  function emojify(s) {
    return s.replace(/\[[^\[\]]+\]/g, function (code) { return EMOJI[code] || code; });
  }

  var POOL = [
    "再看一集就睡[捂脸]", "前方高能", "这段我看了三遍", "女主好飒[比心]", "[吃瓜][吃瓜]", "这配乐绝了",
    "下一集下一集", "已追[爱心]", "说好的最后一集呢", "谁懂啊[流泪]", "男二好帅[色]", "就喜欢这种爽剧",
    "没有广告真舒服[赞]", "1.5倍速刚刚好", "[惊呆]", "打脸来了[奸笑]", "这演技[强]", "撒糖了[害羞]",
    "凌晨三点还在看", "这个反派有点东西", "[送花][送花][送花]", "我宣布这是年度最佳", "又熬夜了[哭]",
    "截图了", "细节拉满", "名场面", "啊啊啊啊啊", "笑死我了[笑哭]"
  ];
  // Moments many viewers send the same words: the app merges them into one
  // comment and shows how many sent it.
  var BURSTS = [
    { at: 8, text: "哈哈哈哈哈哈", n: 9 },
    { at: 30, text: "前方高能[火][火]", n: 14 },
    { at: 52, text: "[笑哭][笑哭][笑哭]", n: 7 },
    { at: 76, text: "终于反转了[鼓掌]", n: 12 },
    { at: 98, text: "爽[爽]", n: 10 },
    { at: 114, text: "下一集！", n: 8 }
  ];
  var LINES = [
    "你以为我还会回头吗？", "这一次，换我来选。", "三年了，你终于肯见我了。", "这份合同，我不签。",
    "从今天起，这里我说了算。", "你到底是谁？", "我等这一天，等了很久。", "别走……再听我说一句。",
    "今晚的事，谁也不许说出去。", "原来一直是你。"
  ];

  // The comments of episode e, merged as the app merges them: the same
  // words within DM_MERGE seconds of the first count towards it.
  function comments(e) {
    var r = rng(e * 7919 + 17), raw = [], t = 0.8 + r();
    while (t < DURATION - 1) {
      raw.push({ at: t, text: POOL[Math.floor(r() * POOL.length)] });
      t += 0.2 + r() * 0.6;
    }
    BURSTS.forEach(function (b) {
      var start = b.at + (r() * 6 - 3);
      for (var i = 0; i < b.n; i++) raw.push({ at: start + (i === 0 ? 0 : r() * 6.5), text: b.text });
    });
    raw.sort(function (a, b) { return a.at - b.at; });
    var items = [];
    raw.forEach(function (c) {
      var text = emojify(c.text);
      for (var k = items.length - 1; k >= 0 && c.at - items[k].at < DM_MERGE; k--) {
        if (items[k].text === text) { items[k].times.push(c.at); return; }
      }
      items.push({ at: c.at, text: text, times: [c.at], lane: -1, w: 0, el: null, shown: 0 });
    });
    return items;
  }

  /* Lanes, as the app gives them: a comment takes the first lane in which it
     neither starts on the one before nor catches up with it, and is left out
     when no lane has room. */
  var dm = { items: [], next: 0, tails: [], width: 0, last: 0, flight: 8, size: 17, live: [] };
  var measureCtx = document.createElement("canvas").getContext("2d");
  var FONT = getComputedStyle(document.body).fontFamily;

  function count(it, pos) {
    var n = 0;
    while (n < it.times.length && it.times[n] <= pos) n++;
    return Math.max(n, 1);
  }
  function measure(it, n) {
    measureCtx.font = "600 " + dm.size + "px " + FONT;
    var w = measureCtx.measureText(it.text).width;
    if (n > 1) {
      measureCtx.font = "700 " + dm.size + "px " + FONT;
      w += measureCtx.measureText(" ×" + it.times.length).width;
    }
    return w;
  }
  function speedOf(w) { return (dm.width + w) / dm.flight; }
  function follows(j, i) {
    var a = dm.items[j], b = dm.items[i];
    var dt = b.at - a.at;
    if (dt >= dm.flight) return true;
    if (dt * speedOf(a.w) < a.w + DM_GAP) return false;
    return dm.width - (dm.flight - dt) * speedOf(b.w) >= DM_GAP;
  }
  function lanesFor(height) {
    var room = Math.max(height - DM_TOP - DM_BOTTOM, 0), lane = dm.size * 1.55, a = DM_AREAS[st.dmArea];
    if (!a.share) return Math.min(a.lanes, Math.max(Math.floor(room / lane), 1));
    return Math.max(Math.floor(room * a.share / lane), 1);
  }
  function firstInFlight(pos) {
    var lo = 0, hi = dm.items.length;
    while (lo < hi) {
      var mid = (lo + hi) >> 1;
      if (dm.items[mid].at > pos - dm.flight) hi = mid; else lo = mid + 1;
    }
    return lo;
  }
  function clearLive() {
    dm.live.forEach(function (it) { if (it.el) { it.el.remove(); it.el = null; } });
    dm.live = [];
  }
  function resetLanes(pos, width, lanes) {
    clearLive();
    dm.width = width;
    dm.tails = [];
    for (var l = 0; l < lanes; l++) dm.tails.push(-1);
    dm.next = firstInFlight(pos);
    for (var i = dm.next; i < dm.items.length; i++) dm.items[i].lane = -1;
  }
  function place(i) {
    var it = dm.items[i];
    it.w = measure(it, it.times.length);
    it.lane = -1;
    for (var l = 0; l < dm.tails.length; l++) {
      var j = dm.tails[l];
      if (j < 0 || follows(j, i)) { it.lane = l; dm.tails[l] = i; return; }
    }
  }
  function advance(pos, width, height) {
    var flight = DM_SPEEDS[st.dmSpeed].flight, size = DM_SIZES[st.dmSize].size;
    var changed = flight !== dm.flight || size !== dm.size;
    dm.flight = flight;
    dm.size = size;
    var lanes = lanesFor(height);
    if (changed || width !== dm.width || lanes !== dm.tails.length || pos < dm.last - 0.25 || pos > dm.last + 2) {
      resetLanes(pos, width, lanes);
      dm.last = pos;
    }
    dm.last = Math.max(dm.last, pos);
    for (; dm.next < dm.items.length && dm.items[dm.next].at <= pos; dm.next++) place(dm.next);
  }
  function drawDanmaku(pos) {
    var width = layer.clientWidth, height = layer.clientHeight;
    if (!st.danmaku || !width) { clearLive(); return; }
    advance(pos, width, height);
    layer.style.opacity = st.opacity;
    var lane = dm.size * 1.55, live = [];
    for (var i = firstInFlight(pos); i < dm.next; i++) {
      var it = dm.items[i];
      if (it.lane < 0) continue;
      if (!it.el) {
        it.el = document.createElement("span");
        it.el.className = "dm";
        it.el.style.fontSize = dm.size + "px";
        it.el.appendChild(document.createTextNode(it.text));
        it.shown = 0;
        layer.appendChild(it.el);
      }
      var n = count(it, pos);
      if (n !== it.shown) {
        if (n > 1) {
          var badge = it.el.querySelector(".n");
          if (!badge) {
            badge = document.createElement("span");
            badge.className = "n";
            it.el.appendChild(badge);
          }
          badge.textContent = "×" + n;
          if (it.shown > 0) {
            badge.classList.add("pop");
            setTimeout(function (b) { b.classList.remove("pop"); }, 160, badge);
          }
        }
        it.shown = n;
      }
      var x = width - ((pos - it.at) / dm.flight) * (width + it.w);
      it.el.style.transform = "translate(" + x.toFixed(1) + "px," + (DM_TOP + it.lane * lane) + "px)";
      live.push(it);
    }
    dm.live.forEach(function (it) {
      if (live.indexOf(it) < 0 && it.el) { it.el.remove(); it.el = null; }
    });
    dm.live = live;
  }

  /* The episode list */
  var eps = $("eps");
  function renderEpisodes() {
    eps.textContent = "";
    var from = st.range * PER_RANGE, to = Math.min(from + PER_RANGE, EPISODES);
    for (var i = from; i < to; i++) {
      var b = document.createElement("button");
      b.type = "button";
      b.className = "ep";
      b.dataset.ep = i;
      var label = "第" + (i + 1) + "集";
      if (i === st.ep) {
        b.classList.add("cur");
        b.setAttribute("aria-current", "true");
        b.innerHTML = '<span class="bars"><i></i><i></i><i></i><i></i><i></i></span>';
      } else {
        b.textContent = i + 1;
        var p = st.progress[i] || 0;
        if (LOCKED[i]) {
          b.classList.add("locked");
          b.insertAdjacentHTML("beforeend", '<svg class="mark" viewBox="0 0 24 24"><path d="M7 10V7.5a5 5 0 0 1 10 0V10h1a1 1 0 0 1 1 1v9a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1v-9a1 1 0 0 1 1-1zm2.5 0h5V7.5a2.5 2.5 0 0 0-5 0z"/></svg>');
          label += "，暂不可播放";
        } else if (p >= 1) {
          b.classList.add("done");
          b.insertAdjacentHTML("beforeend", '<svg class="mark" viewBox="0 0 24 24"><path d="M9.5 16.2 5.3 12l-2 2 6.2 6.2L21 8.7l-2-2z"/></svg>');
          label += "，已看完";
        } else if (p > 0.02) {
          b.insertAdjacentHTML("beforeend", '<i class="part" style="width:' + (p * 100) + '%"></i>');
          label += "，看到" + Math.floor(p * 100) + "%";
        }
      }
      b.setAttribute("aria-label", label);
      eps.appendChild(b);
    }
    document.querySelectorAll("#ranges button").forEach(function (r) {
      r.classList.toggle("on", +r.dataset.range === st.range);
    });
    $("eps-now").textContent = "第" + (st.ep + 1) + "集 / 共" + EPISODES + "集";
    $("ep-line").textContent = "第" + (st.ep + 1) + "集 · 共" + EPISODES + "集";
    $("prev").disabled = st.ep === 0;
    $("next").disabled = st.ep >= EPISODES - 1;
  }
  eps.addEventListener("click", function (e) {
    var b = e.target.closest(".ep");
    if (!b) return;
    var i = +b.dataset.ep;
    if (i === st.ep || LOCKED[i]) return;
    var p = st.progress[i] || 0;
    open(i, p > 0 && p < 1 ? p * DURATION : 0);
    play();
  });
  $("ranges").addEventListener("click", function (e) {
    var b = e.target.closest("button");
    if (!b) return;
    st.range = +b.dataset.range;
    renderEpisodes();
  });

  function open(i, at) {
    if (i < 0 || i >= EPISODES || LOCKED[i]) return false;
    st.progress[st.ep] = Math.max(st.progress[st.ep] || 0, st.pos >= DURATION - 3 ? 1 : st.pos / DURATION);
    st.ep = i;
    st.pos = at || 0;
    st.range = Math.floor(i / PER_RANGE);
    dm.items = comments(i);
    dm.width = 0;
    clearLive();
    scene.style.filter = "hue-rotate(" + ((i * 37) % 70 - 35) + "deg)";
    renderEpisodes();
    render();
    return true;
  }
  function step(d) {
    var i = st.ep + d;
    while (LOCKED[i]) i += d;
    if (open(i, 0)) play();
  }

  /* Playing */
  var lastFrame = 0, raf = 0, visible = false;
  function loop(now) {
    raf = 0;
    if (!st.playing) return;
    var dt = lastFrame ? Math.min((now - lastFrame) / 1000, 0.1) : 0;
    lastFrame = now;
    st.pos += dt * (st.boost ? 2 : st.rate);
    if (st.pos >= DURATION) {
      st.pos = DURATION;
      ended();
    }
    render();
    if (st.playing && visible) raf = requestAnimationFrame(loop);
  }
  function kick() {
    if (!raf && st.playing && visible) {
      lastFrame = 0;
      raf = requestAnimationFrame(loop);
    }
  }
  function play() {
    st.playing = true;
    st.userPaused = false;
    if (st.pos >= DURATION) st.pos = 0;
    syncPlaying();
    kick();
  }
  function pause(byUser) {
    st.playing = false;
    if (byUser) st.userPaused = true;
    syncPlaying();
  }
  function togglePlay() { st.playing ? pause(true) : play(); }
  function ended() {
    st.progress[st.ep] = 1;
    var i = st.ep + 1;
    while (LOCKED[i]) i++;
    if (i < EPISODES) open(i, 0); else pause(false);
  }
  function syncPlaying() {
    app.classList.toggle("playing", st.playing);
    var label = st.playing ? "暂停" : "播放";
    playBtn.setAttribute("aria-label", label);
    playBtn.dataset.tip = label + " (空格)";
    updateChrome();
  }

  function render() {
    var pos = st.pos, f = pos / DURATION;
    seekFill.style.width = (f * 100) + "%";
    seekKnob.style.left = (f * 100) + "%";
    seek.setAttribute("aria-valuenow", Math.floor(pos));
    seek.setAttribute("aria-valuetext", fmt(pos) + "，共" + fmt(DURATION));
    $("time-now").textContent = fmt(pos);
    // The picture drifts slowly, in the episode's time: it stops as it pauses.
    var k = 1.06 + 0.04 * Math.sin(pos / 9);
    scene.style.transform = "scale(" + k.toFixed(4) + ") translateY(" + (Math.sin(pos / 13) * 1.2).toFixed(3) + "%)";
    var line = Math.floor(pos / 5.5), inLine = pos % 5.5;
    subtitle.textContent = LINES[(line + st.ep * 3) % LINES.length];
    subtitle.style.opacity = inLine > 4.6 ? 0 : 1;
    drawDanmaku(pos);
  }

  /* Seek bar */
  var dragging = false;
  function seekAt(clientX) {
    var r = seek.getBoundingClientRect();
    var x = Math.min(Math.max(clientX - r.left, 0), r.width);
    return { x: x, w: r.width, t: (x / r.width) * DURATION };
  }
  function showTip(clientX) {
    var s = seekAt(clientX);
    seekTip.textContent = fmt(s.t);
    seekTip.style.left = Math.min(Math.max(s.x - 28, 0), s.w - 56) + "px";
  }
  seek.addEventListener("pointermove", function (e) {
    showTip(e.clientX);
    if (dragging) { st.pos = seekAt(e.clientX).t; render(); }
  });
  seek.addEventListener("pointerdown", function (e) {
    dragging = true;
    seek.classList.add("drag");
    seek.setPointerCapture(e.pointerId);
    st.pos = seekAt(e.clientX).t;
    showTip(e.clientX);
    render();
  });
  function endDrag() {
    dragging = false;
    seek.classList.remove("drag");
  }
  seek.addEventListener("pointerup", endDrag);
  seek.addEventListener("pointercancel", endDrag);
  seek.addEventListener("keydown", function (e) {
    if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
      seekBy(e.key === "ArrowLeft" ? -5 : 5);
      e.preventDefault();
      e.stopPropagation();
    }
  });
  function seekBy(d) {
    st.pos = Math.min(Math.max(st.pos + d, 0), DURATION - 0.1);
    render();
  }

  /* Toast */
  var toastTimer = 0;
  function toast(text) {
    toastEl.textContent = text;
    toastEl.classList.add("show");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () { toastEl.classList.remove("show"); }, 1600);
  }

  /* Chrome: shown while paused, and while the pointer moves over the stage */
  var hover = false, movedAt = 0, keyAt = 0;
  function popOpen() { return !$("speed-pop").hidden || !$("dm-pop").hidden; }
  function updateChrome() {
    var now = Date.now();
    var shown = !st.playing || (hover && now - movedAt < CHROME_LINGER) || now - keyAt < KEY_LINGER ||
      dragging || popOpen() || stage.contains(document.activeElement) && document.activeElement !== app;
    stage.classList.toggle("idle", !shown);
  }
  stage.addEventListener("pointermove", function () { hover = true; movedAt = Date.now(); updateChrome(); });
  stage.addEventListener("pointerleave", function () { hover = false; updateChrome(); });
  setInterval(updateChrome, 250);

  /* Clicks on the picture: one pauses, two go full screen */
  var clickTimer = 0;
  $("picture").addEventListener("click", function () {
    if (clickTimer) return;
    clickTimer = setTimeout(function () { clickTimer = 0; togglePlay(); }, CLICK_DELAY);
  });
  $("picture").addEventListener("dblclick", function () {
    clearTimeout(clickTimer);
    clickTimer = 0;
    toggleFull();
  });

  /* Buttons */
  playBtn.addEventListener("click", togglePlay);
  $("prev").addEventListener("click", function () { step(-1); });
  $("next").addEventListener("click", function () { step(1); });

  function toggleDanmaku() {
    st.danmaku = !st.danmaku;
    var b = $("dm-btn");
    b.classList.toggle("on", st.danmaku);
    b.setAttribute("aria-pressed", st.danmaku);
    $("dm-set-wrap").style.visibility = st.danmaku ? "" : "hidden";
    if (!st.danmaku) closePop("dm-pop", "dm-set");
    toast(st.danmaku ? "已开启弹幕" : "已关闭弹幕");
    render();
  }
  $("dm-btn").addEventListener("click", toggleDanmaku);

  function toggleMute() {
    st.muted = !st.muted;
    app.classList.toggle("muted", st.muted);
    toast(st.muted ? "已静音" : "音量 100%");
  }
  $("mute").addEventListener("click", toggleMute);

  function togglePin() {
    var b = $("pin"), on = b.getAttribute("aria-pressed") !== "true";
    b.setAttribute("aria-pressed", on);
    b.classList.toggle("on", on);
  }
  $("pin").addEventListener("click", togglePin);

  function toggleFull() {
    if (document.fullscreenElement) {
      document.exitFullscreen();
    } else if (app.requestFullscreen) {
      app.requestFullscreen().catch(function () {});
    }
  }
  $("full").addEventListener("click", toggleFull);
  document.addEventListener("fullscreenchange", function () {
    var full = document.fullscreenElement === app;
    $("full").dataset.tip = full ? "退出全屏 (F)" : "全屏 (F)";
    dm.width = 0;
    render();
  });

  $("panel-btn").addEventListener("click", function () {
    var shown = app.classList.toggle("no-panel") === false;
    this.setAttribute("aria-expanded", shown);
    dm.width = 0;
    render();
  });

  $("follow").addEventListener("click", function () {
    var on = this.classList.toggle("on");
    this.querySelector("span").textContent = on ? "已追剧" : "追剧";
    toast(on ? "已加入追剧" : "已取消追剧");
  });
  $("desc").addEventListener("click", function () { this.classList.toggle("open"); });

  /* Popovers */
  function openPop(pop, btn) {
    $(pop).hidden = false;
    $(btn).setAttribute("aria-expanded", "true");
    $(btn).parentNode.setAttribute("data-open", "");
  }
  function closePop(pop, btn) {
    $(pop).hidden = true;
    $(btn).setAttribute("aria-expanded", "false");
    $(btn).parentNode.removeAttribute("data-open");
  }
  function togglePop(pop, btn) {
    var open = $(pop).hidden;
    closePop("speed-pop", "speed");
    closePop("dm-pop", "dm-set");
    if (open) openPop(pop, btn);
  }
  document.addEventListener("pointerdown", function (e) {
    if (!e.target.closest(".pop-anchor")) {
      closePop("speed-pop", "speed");
      closePop("dm-pop", "dm-set");
    }
  });

  // Speeds, the fastest on top.
  var speedPop = $("speed-pop");
  function renderSpeeds() {
    speedPop.textContent = "";
    SPEEDS.slice().reverse().forEach(function (s) {
      var b = document.createElement("button");
      b.type = "button";
      b.setAttribute("role", "option");
      b.setAttribute("aria-selected", s === st.rate);
      b.textContent = s + "x";
      b.addEventListener("click", function () { setSpeed(s); closePop("speed-pop", "speed"); });
      speedPop.appendChild(b);
    });
    $("speed").textContent = st.rate + "x";
  }
  function setSpeed(s) {
    st.rate = s;
    renderSpeeds();
    toast(s + "x 倍速");
  }
  function stepSpeed(d) {
    var i = SPEEDS.indexOf(st.rate) + d;
    if (i >= 0 && i < SPEEDS.length) setSpeed(SPEEDS[i]);
  }
  $("speed").addEventListener("click", function () { togglePop("speed-pop", "speed"); });
  renderSpeeds();

  // Danmaku settings: sliders with a dot at each step, like the app's.
  var sliders = [
    { key: "opacity", name: "不透明度", min: 0.1, max: 1, step: 0.05, def: 1, text: function (v) { return Math.round(v * 100) + "%"; } },
    { key: "dmSpeed", name: "移动速度", min: 0, max: 4, step: 1, def: 2, steps: DM_SPEEDS },
    { key: "dmArea", name: "显示区域", min: 0, max: 4, step: 1, def: 2, steps: DM_AREAS },
    { key: "dmSize", name: "字体大小", min: 0, max: 4, step: 1, def: 2, steps: DM_SIZES }
  ];
  var holder = $("dm-sliders");
  sliders.forEach(function (s) {
    var row = document.createElement("div");
    row.className = "setting";
    row.innerHTML = '<div class="setting-head"><span>' + s.name + '</span><em></em></div>' +
      '<div class="slider" role="slider" tabindex="0" aria-label="' + s.name + '"><i class="rail"></i><i class="fill"></i><i class="thumb"></i></div>';
    var el = row.querySelector(".slider");
    if (s.steps) {
      for (var k = 0; k <= 4; k++) {
        var d = document.createElement("i");
        d.className = "dot";
        d.style.left = "calc(8px + (100% - 16px) * " + (k / 4) + ")";
        el.insertBefore(d, el.querySelector(".thumb"));
      }
    }
    s.row = row;
    s.el = el;
    function setFrom(clientX) {
      var r = el.getBoundingClientRect();
      var f = Math.min(Math.max((clientX - r.left - 8) / (r.width - 16), 0), 1);
      var v = s.min + f * (s.max - s.min);
      v = Math.round(v / s.step) * s.step;
      st[s.key] = +v.toFixed(2);
      syncSliders();
      render();
    }
    el.addEventListener("pointerdown", function (e) {
      el.classList.add("drag");
      el.setPointerCapture(e.pointerId);
      setFrom(e.clientX);
    });
    el.addEventListener("pointermove", function (e) { if (el.classList.contains("drag")) setFrom(e.clientX); });
    el.addEventListener("pointerup", function () { el.classList.remove("drag"); });
    el.addEventListener("keydown", function (e) {
      var d = e.key === "ArrowRight" || e.key === "ArrowUp" ? 1 : e.key === "ArrowLeft" || e.key === "ArrowDown" ? -1 : 0;
      if (!d) return;
      st[s.key] = +Math.min(Math.max(st[s.key] + d * s.step, s.min), s.max).toFixed(2);
      syncSliders();
      render();
      e.preventDefault();
      e.stopPropagation();
    });
    holder.appendChild(row);
  });
  function syncSliders() {
    var isDefault = true;
    sliders.forEach(function (s) {
      var v = st[s.key], f = (v - s.min) / (s.max - s.min);
      var text = s.steps ? s.steps[v].label : s.text(v);
      s.row.querySelector("em").textContent = text;
      s.el.setAttribute("aria-valuetext", text);
      s.el.querySelector(".fill").style.width = "calc(8px + (100% - 16px) * " + f + ")";
      s.el.querySelector(".thumb").style.left = "calc(8px + (100% - 16px) * " + f + ")";
      if (v !== s.def) isDefault = false;
    });
    $("dm-reset").disabled = isDefault;
  }
  $("dm-reset").addEventListener("click", function () {
    sliders.forEach(function (s) { st[s.key] = s.def; });
    syncSliders();
    render();
  });
  $("dm-set").addEventListener("click", function () { togglePop("dm-pop", "dm-set"); });
  syncSliders();

  /* Keys, while the player is pointed at or has the focus */
  var rightAt = 0, boostTimer = 0;
  function active() { return hover || app.contains(document.activeElement); }
  document.addEventListener("keydown", function (e) {
    if (!active() || e.metaKey || e.ctrlKey || e.altKey) return;
    var t = e.target;
    if (t !== app && t.closest && t.closest(".slider, .seek") && /Arrow/.test(e.key)) return;
    var handled = true;
    switch (e.key) {
      case " ":
        if (t.tagName === "BUTTON" && t !== playBtn) { handled = false; break; }
        if (!e.repeat) togglePlay();
        break;
      case "ArrowLeft": seekBy(-5); break;
      case "ArrowRight":
        if (!rightAt) {
          rightAt = Date.now();
          boostTimer = setTimeout(function () {
            st.boost = true;
            boostEl.hidden = false;
            if (!st.playing) play();
          }, BOOST_DELAY);
        }
        break;
      case "ArrowUp": step(-1); break;
      case "ArrowDown": step(1); break;
      case "d": case "D": toggleDanmaku(); break;
      case "m": case "M": toggleMute(); break;
      case "f": case "F": toggleFull(); break;
      case "t": case "T": togglePin(); break;
      case "[": stepSpeed(-1); break;
      case "]": stepSpeed(1); break;
      case "Escape":
        if (popOpen()) { closePop("speed-pop", "speed"); closePop("dm-pop", "dm-set"); } else handled = false;
        break;
      default: handled = false;
    }
    if (handled) {
      keyAt = Date.now();
      e.preventDefault();
      updateChrome();
    }
  });
  document.addEventListener("keyup", function (e) {
    if (e.key !== "ArrowRight" || !rightAt) return;
    clearTimeout(boostTimer);
    if (st.boost) {
      st.boost = false;
      boostEl.hidden = true;
    } else if (active()) {
      seekBy(5);
    }
    rightAt = 0;
  });

  /* Plays while in view, unless the user paused it */
  var reduce = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  if ("IntersectionObserver" in window) {
    new IntersectionObserver(function (entries) {
      visible = entries[0].isIntersecting;
      if (visible && !st.userPaused && !reduce) play(); else kick();
    }, { threshold: 0.35 }).observe(app);
  } else {
    visible = true;
  }
  document.addEventListener("visibilitychange", function () {
    if (document.hidden) { lastFrame = 0; } else { kick(); }
  });
  window.addEventListener("resize", function () { render(); });

  open(12, 72);
  syncPlaying();
})();
