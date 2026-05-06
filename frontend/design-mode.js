// design-mode.js — Phase A placeholder
(function () {
  var meta = document.getElementById('crit-design-meta');
  var proxyPort = meta ? parseInt(meta.dataset.proxyPort, 10) : 0;
  document.title = 'crit design (Phase A)';
  var b = document.createElement('div');
  b.style.cssText = 'position:fixed;top:0;left:0;right:0;background:#ffd700;color:#000;padding:8px;text-align:center;font:14px monospace;z-index:9999';
  b.textContent = 'crit design mode — Phase A end. proxy port: ' + proxyPort;
  document.body.appendChild(b);
  if (proxyPort) {
    var f = document.createElement('iframe');
    f.src = 'http://localhost:' + proxyPort + '/';
    f.style.cssText = 'position:fixed;top:36px;left:0;right:0;bottom:0;width:100%;height:calc(100vh - 36px);border:none';
    document.body.appendChild(f);
  }
})();
