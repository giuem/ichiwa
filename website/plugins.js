// The plugin page: lists the plugins in plugins.json, the index the
// plugin-index workflow publishes with the site (docs/plugin-design.md,
// section 13). Everything in the index comes from third-party plugins, so
// none of it is ever parsed as HTML: text goes in through textContent,
// links are built from a checked owner/repo, and icons are the plugin's
// re-encoded PNG on this site or the owner's GitHub avatar.
(function () {
  "use strict";

  var INDEX_VERSION = 1;
  var API_VERSION = 1;
  // Fewer plugins than this fit on a screen or two: no search box.
  var SEARCH_FROM = 7;
  // Hosts shown before "等 N 个网站".
  var HOSTS_SHOWN = 3;

  var tools = document.getElementById("tools");
  var q = document.getElementById("q");
  var count = document.getElementById("count");
  var status = document.getElementById("status");
  var statusText = document.getElementById("status-text");
  var statusFine = document.getElementById("status-fine");
  var retry = document.getElementById("retry");
  var cards = document.getElementById("cards");

  var plugins = [];
  // Set by "重试": where focus goes once the list or a message is shown.
  var refocus = false;

  /* ---------- Checking the index ---------- */

  // Line breaks and other control characters become spaces. Invisible
  // format characters (bidirectional controls, zero-width spaces, tag
  // characters) and the Hangul fillers that render as blanks are dropped,
  // so text reads as what it is; ZWNJ and ZWJ stay, as emoji and some
  // scripts need them.
  var BREAKS = /[\u0000-\u001f\u007f-\u009f\u2028\u2029]/g;
  var INVISIBLE = /(?![\u200c\u200d])[\p{Cf}\u115f\u1160\u3164\uffa0]/gu;

  function str(v, max) {
    if (typeof v !== "string") return "";
    v = v.replace(BREAKS, " ").replace(INVISIBLE, "").replace(/\s+/g, " ").trim();
    if (!max) return v;
    var chars = graphemes(v);
    return chars.length > max ? chars.slice(0, max).join("") + "…" : v;
  }
  // What a reader counts as one character, so a cut never splits an emoji.
  var segmenter = typeof Intl.Segmenter === "function" ? new Intl.Segmenter("zh-CN", { granularity: "grapheme" }) : null;
  function graphemes(v) {
    return segmenter ? Array.from(segmenter.segment(v), function (s) { return s.segment; }) : Array.from(v);
  }

  // GitHub's own rules for user and repository names.
  var REPO = /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})\/[A-Za-z0-9._-]{1,100}$/;
  var ID = /^[a-z0-9](?:[a-z0-9._-]{0,62}[a-z0-9])?$/;

  // One entry of permissions.network, by the app's own rules
  // (internal/netguard ValidPattern), less the public-suffix check this page
  // has no list for. The index only holds entries the app accepted; this
  // check only decides how an entry is shown, never whether.
  function validHost(h) {
    if (typeof h !== "string" || h === "" || h !== h.trim().toLowerCase()) return false;
    var host = h.indexOf("*.") === 0 ? h.slice(2) : h;
    if (/[:/[\]@*]/.test(host)) return false;
    if (host === "localhost" || /\.localhost$/.test(host)) return false;
    if (host.length > 253 || host.indexOf(".") < 0) return false;
    var labels = host.split(".");
    for (var i = 0; i < labels.length; i++) {
      if (!/^[a-z0-9_](?:[a-z0-9_-]{0,61}[a-z0-9_])?$/.test(labels[i])) return false;
    }
    // No top-level domain is a number: "8.526344" and "0x08.0x08.0x08.0x08"
    // are IPv4 addresses to a resolver.
    return !/^(?:[0-9]+|0x[0-9a-f]*)$/.test(labels[labels.length - 1]);
  }
  // The hosts as the card lists them: every entry, in plain text, the ones
  // this page doesn't recognise first so "等 N 个网站" can't hide them.
  // null when the list itself can't be read.
  function hostList(v) {
    if (!Array.isArray(v)) return null;
    var odd = [], ok = [];
    v.forEach(function (h) {
      if (validHost(h)) ok.push({ text: h });
      else odd.push({ text: typeof h !== "string" ? "（不是文字）" : str(h, 80) || "（空）", odd: true });
    });
    return odd.concat(ok);
  }

  // Returns the plugin as the page shows it, or null when it can't be shown.
  function clean(p) {
    if (!p || typeof p !== "object") return null;
    var repo = str(p.repo);
    var name = str(p.name, 32);
    if (!REPO.test(repo) || /\/\.\.?$/.test(repo) || !name) return null;
    var id = str(p.id);
    var date = new Date(str(p.publishedAt));
    var network = hostList(p.network);
    return {
      id: id,
      name: name,
      description: str(p.description, 200),
      icon: ID.test(id) && p.icon === "plugin-icons/" + id + ".png" ? p.icon : "",
      repo: repo,
      key: repo.toLowerCase(),
      owner: repo.split("/")[0],
      version: str(p.version, 40),
      published: isNaN(date) ? null : date,
      license: str(p.license, 64),
      minAppVersion: str(p.minAppVersion, 20),
      network: network,
      supported: p.kind === "source" && p.apiVersion === API_VERSION && network !== null,
      knownKind: p.kind === "source"
    };
  }

  /* ---------- Rendering ---------- */

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  }

  var dateFormat = new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "long", day: "numeric" });

  var GITHUB = '<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/></svg>';
  var DOWNLOAD = '<svg viewBox="0 0 24 24" aria-hidden="true" class="stroke"><path d="M12 4v11m-5-5 5 5 5-5M5 20h14"/></svg>';

  // Our own markup, never anything from the index.
  function icon(svg) {
    var t = document.createElement("template");
    t.innerHTML = svg;
    return t.content.firstChild;
  }

  function card(p) {
    var li = el("li", "card");

    var head = el("div", "card-head");
    head.appendChild(pluginIcon(p));
    var titles = el("div", "card-titles");
    titles.appendChild(el("h3", "plugin-name", p.name));
    var repo = el("a", "plugin-repo", p.repo);
    repo.href = "https://github.com/" + p.repo;
    titles.appendChild(repo);
    head.appendChild(titles);
    li.appendChild(head);

    var desc = el("p", "plugin-desc" + (p.description ? "" : " none"), p.description || "没有简介。");
    if (p.description) desc.title = p.description;
    li.appendChild(desc);

    var meta = [];
    if (p.version) meta.push(p.version);
    if (p.license) meta.push(p.license);
    if (p.published) meta.push(dateFormat.format(p.published) + "更新");
    if (meta.length) li.appendChild(el("p", "plugin-meta", meta.join(" · ")));
    li.appendChild(hosts(p.network));
    if (p.network === null) {
      li.appendChild(el("p", "plugin-meta note", "列表里的信息不完整，暂时不能安装"));
    } else if (!p.supported) {
      li.appendChild(el("p", "plugin-meta note", p.knownKind ? "需要更新一集才能安装" : "一集还不支持这类插件"));
    } else if (p.minAppVersion) {
      li.appendChild(el("p", "plugin-meta note", "需要一集 " + p.minAppVersion + " 或更新"));
    }

    var actions = el("div", "plugin-actions");
    if (p.supported) {
      var install = el("a", "action primary");
      install.href = "ichiwa://install-plugin?repo=" + p.repo;
      install.setAttribute("aria-label", "在一集里安装 " + p.name);
      install.append(icon(DOWNLOAD), "安装");
      actions.appendChild(install);
    } else {
      actions.classList.add("single");
    }
    var gh = el("a", "action");
    gh.href = "https://github.com/" + p.repo;
    gh.setAttribute("aria-label", p.name + " 的 GitHub 仓库");
    gh.append(icon(GITHUB), "GitHub");
    actions.appendChild(gh);
    li.appendChild(actions);
    return li;
  }

  // "访问 a、b、c 等 N 个网站", with a button to show them all: a long list
  // can't hide a host where only hover would reveal it.
  function hosts(list) {
    var p = el("p", "plugin-hosts");
    if (list === null) {
      p.textContent = "要访问的网站读不出来";
      return p;
    }
    if (!list.length) {
      p.textContent = "不访问网络";
      return p;
    }
    var text = el("span");
    p.appendChild(text);
    function show(items) {
      text.replaceChildren("访问 ");
      items.forEach(function (h, i) {
        if (i) text.append("、");
        text.append(h.text);
        if (h.odd) text.appendChild(el("em", "odd", "（无法识别）"));
      });
    }
    var odd = list.filter(function (h) { return h.odd; }).length;
    if (list.length <= HOSTS_SHOWN) {
      show(list);
    } else {
      show(list.slice(0, HOSTS_SHOWN));
      text.append(" 等 " + list.length + " 个网站");
      var more = el("button", "more", "全部");
      more.type = "button";
      more.setAttribute("aria-label", "显示全部 " + list.length + " 个网站");
      more.addEventListener("click", function () {
        show(list);
        more.remove();
        text.tabIndex = -1;
        text.focus();
      });
      p.append(" ", more);
    }
    if (odd) p.appendChild(el("span", "odd-note", "其中 " + odd + " 个地址这里认不出来，安装前在一集里核对"));
    return p;
  }

  // The plugin's icon, else the owner's GitHub avatar, else the first
  // character of the name. Each image is asked for once: cards are built
  // once and searching only hides them.
  function pluginIcon(p) {
    var srcs = [];
    if (p.icon) srcs.push(p.icon);
    srcs.push("https://avatars.githubusercontent.com/" + p.owner + "?s=80");
    var img = el("img", "plugin-icon");
    img.alt = "";
    img.width = 40;
    img.height = 40;
    img.loading = "lazy";
    img.decoding = "async";
    img.referrerPolicy = "no-referrer";
    img.addEventListener("error", function () {
      if (srcs.length) img.src = srcs.shift();
      else img.replaceWith(letter(p.name));
    });
    img.src = srcs.shift();
    return img;
  }
  function letter(name) {
    var s = el("span", "plugin-icon letter", graphemes(name)[0].toUpperCase());
    s.setAttribute("aria-hidden", "true");
    return s;
  }

  function showStatus(text, fine, canRetry) {
    status.hidden = false;
    statusText.textContent = text;
    statusFine.textContent = fine || "";
    statusFine.hidden = !fine;
    retry.hidden = !canRetry;
  }

  function filter() {
    var words = q.value.toLowerCase().split(/\s+/).filter(Boolean);
    var n = 0;
    plugins.forEach(function (p) {
      var hit = words.every(function (w) { return p.hay.indexOf(w) >= 0; });
      p.node.hidden = !hit;
      if (hit) n++;
    });
    cards.hidden = !n;
    count.textContent = words.length ? "找到 " + n + " 个插件" : "共 " + plugins.length + " 个插件";
    if (n) status.hidden = true;
    else showStatus("没有找到符合条件的插件。", "换个词试试。", false);
  }

  /* ---------- Loading ---------- */

  function fail() {
    status.classList.remove("loading");
    plugins = [];
    tools.hidden = true;
    cards.hidden = true;
    showStatus("插件列表读不出来。", "检查一下网络，稍后再试。", true);
  }

  function load() {
    showStatus("正在读取插件列表…", "", false);
    status.classList.add("loading");
    fetch("plugins.json", { cache: "no-cache" })
      .then(function (r) {
        if (!r.ok) throw new Error("HTTP " + r.status);
        return r.json();
      })
      .then(function (index) {
        if (!index || typeof index !== "object") throw new Error("bad index");
        status.classList.remove("loading");
        // The version first: a newer format may not have "plugins" at all.
        if (index.version !== INDEX_VERSION) {
          showStatus("插件列表的格式更新了。", "刷新页面再试试。", false);
          return;
        }
        if (!Array.isArray(index.plugins)) throw new Error("bad index");
        var seen = {}, blockedIds = {};
        var blocked = index.blocked && typeof index.blocked === "object" ? index.blocked : {};
        (Array.isArray(blocked.repos) ? blocked.repos : []).forEach(function (r) {
          if (typeof r === "string") seen[r.toLowerCase()] = true;
        });
        (Array.isArray(blocked.ids) ? blocked.ids : []).forEach(function (id) {
          if (typeof id === "string") blockedIds[id] = true;
        });
        plugins = index.plugins.map(clean).filter(function (p) {
          // GitHub names ignore case: Owner/Repo and owner/repo are one.
          if (!p || seen[p.key] || blockedIds[p.id]) return false;
          seen[p.key] = true;
          return true;
        });
        plugins.sort(function (a, b) { return a.name.localeCompare(b.name, "zh-CN"); });
        if (!plugins.length) {
          showStatus("还没有插件。", "写了插件的话，照下面的步骤发布，就会出现在这里。", false);
          return;
        }
        plugins.forEach(function (p) {
          p.node = card(p);
          p.hay = (p.name + "\n" + p.description + "\n" + p.repo + "\n" + p.id).toLowerCase();
        });
        cards.replaceChildren.apply(cards, plugins.map(function (p) { return p.node; }));
        tools.hidden = plugins.length < SEARCH_FROM;
        filter();
        if (refocus) cards.focus();
        refocus = false;
      })
      .catch(fail);
  }
  retry.addEventListener("click", function () {
    // The button hides while loading: keep focus on the message, then on
    // the list once it shows.
    refocus = true;
    status.focus();
    load();
  });
  q.addEventListener("input", function (e) {
    if (!e.isComposing) filter();
  });
  q.addEventListener("compositionend", filter);
  load();

  /* ---------- Copy ---------- */

  document.querySelectorAll(".copy").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var text = btn.parentNode.querySelector("code").textContent;
      var done = navigator.clipboard && window.isSecureContext ? navigator.clipboard.writeText(text) : Promise.reject();
      done.then(function () {
        btn.textContent = "已复制";
        setTimeout(function () { btn.textContent = "复制"; }, 1600);
      }, function () {
        btn.textContent = "请手动复制";
      });
    });
  });
})();
