// The plugin page: lists the plugins in plugins.json, the index the
// plugin-index workflow publishes with the site (docs/plugin-design.md,
// section 13). Everything in the index comes from third-party plugins, so
// none of it is ever parsed as HTML: text goes in through textContent,
// links are built from a checked owner/repo, and icons are files under
// plugin-icons/ on this site or the owner's GitHub avatar.
(function () {
  "use strict";

  var INDEX_VERSION = 1;
  // Fewer plugins than this fit on a screen or two: no search box.
  var SEARCH_FROM = 7;

  var tools = document.getElementById("tools");
  var q = document.getElementById("q");
  var count = document.getElementById("count");
  var status = document.getElementById("status");
  var statusText = document.getElementById("status-text");
  var statusFine = document.getElementById("status-fine");
  var retry = document.getElementById("retry");
  var cards = document.getElementById("cards");

  var plugins = [];

  /* ---------- Checking the index ---------- */

  function str(v, max) {
    if (typeof v !== "string") return "";
    v = v.replace(/[\u0000-\u001f\u007f]/g, " ").trim();
    return max && v.length > max ? v.slice(0, max) + "…" : v;
  }
  function strs(v, max) {
    return Array.isArray(v) ? v.map(function (s) { return str(s, max); }).filter(Boolean) : [];
  }
  // GitHub's own rules for user and repository names.
  var REPO = /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})\/[A-Za-z0-9._-]{1,100}$/;
  var ICON = /^plugin-icons\/[a-z0-9][a-z0-9._-]{0,80}\.(?:png|jpe?g|webp)$/;

  // Returns the plugin as the page shows it, or null when it can't be shown.
  function clean(p) {
    if (!p || typeof p !== "object") return null;
    var repo = str(p.repo);
    var name = str(p.name, 32);
    if (!REPO.test(repo) || /\/\.\.?$/.test(repo) || !name) return null;
    var date = new Date(str(p.publishedAt));
    return {
      id: str(p.id, 64),
      name: name,
      description: str(p.description, 200),
      icon: ICON.test(str(p.icon)) ? str(p.icon) : "",
      repo: repo,
      owner: repo.split("/")[0],
      version: str(p.version, 40),
      published: isNaN(date) ? null : date,
      license: str(p.license, 64),
      minAppVersion: str(p.minAppVersion, 20),
      network: strs(p.network, 253)
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
    var hosts = el("p", "plugin-hosts");
    hosts.textContent = p.network.length ? "访问 " + p.network.join("、") : "不访问网络";
    hosts.title = hosts.textContent;
    li.appendChild(hosts);
    if (p.minAppVersion) li.appendChild(el("p", "plugin-meta needs", "需要一集 " + p.minAppVersion + " 或更新"));

    var actions = el("div", "plugin-actions");
    var install = el("a", "action primary");
    install.href = "ichiwa://install-plugin?repo=" + p.repo;
    install.setAttribute("aria-label", "在一集里安装 " + p.name);
    install.append(icon(DOWNLOAD), "安装");
    var gh = el("a", "action");
    gh.href = "https://github.com/" + p.repo;
    gh.setAttribute("aria-label", p.name + " 的 GitHub 仓库");
    gh.append(icon(GITHUB), "GitHub");
    actions.append(install, gh);
    li.appendChild(actions);
    return li;
  }

  // The manifest's icon, else the owner's GitHub avatar, else the first
  // character of the name.
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
    var s = el("span", "plugin-icon letter", Array.from(name)[0].toUpperCase());
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

  function render() {
    var words = q.value.toLowerCase().split(/\s+/).filter(Boolean);
    var shown = plugins.filter(function (p) {
      var hay = (p.name + "\n" + p.description + "\n" + p.repo + "\n" + p.id).toLowerCase();
      return words.every(function (w) { return hay.indexOf(w) >= 0; });
    });
    cards.replaceChildren.apply(cards, shown.map(card));
    cards.hidden = !shown.length;
    count.textContent = words.length ? "找到 " + shown.length + " 个插件" : "共 " + plugins.length + " 个插件";
    if (shown.length) status.hidden = true;
    else showStatus("没有找到符合条件的插件。", "换个词试试。", false);
  }

  /* ---------- Loading ---------- */

  function load() {
    showStatus("正在读取插件列表…", "", false);
    status.classList.add("loading");
    fetch("plugins.json", { cache: "no-cache" })
      .then(function (r) {
        if (!r.ok) throw new Error("HTTP " + r.status);
        return r.json();
      })
      .then(function (index) {
        status.classList.remove("loading");
        if (!index || typeof index !== "object" || !Array.isArray(index.plugins)) throw new Error("bad index");
        if (index.version !== INDEX_VERSION) {
          showStatus("插件列表的格式更新了。", "刷新页面再试试。", false);
          return;
        }
        var seen = {};
        plugins = index.plugins.map(clean).filter(function (p) {
          if (!p || seen[p.repo]) return false;
          seen[p.repo] = true;
          return true;
        });
        plugins.sort(function (a, b) { return a.name.localeCompare(b.name, "zh-CN"); });
        if (!plugins.length) {
          showStatus("还没有插件。", "写了插件的话，照下面的步骤发布，就会出现在这里。", false);
          return;
        }
        tools.hidden = plugins.length < SEARCH_FROM;
        render();
      })
      .catch(function () {
        status.classList.remove("loading");
        plugins = [];
        tools.hidden = true;
        cards.hidden = true;
        showStatus("插件列表读不出来。", "检查一下网络，稍后再试。", true);
      });
  }
  retry.addEventListener("click", load);
  q.addEventListener("input", render);
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
