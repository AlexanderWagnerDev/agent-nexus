(function () {
  const KEY = 'nexus-theme';
  const media = window.matchMedia('(prefers-color-scheme: dark)');

  function normalize(mode) {
    return mode === 'dark' || mode === 'light' ? mode : 'system';
  }

  function resolved(mode) {
    const m = normalize(mode);
    if (m === 'system') return media.matches ? 'dark' : 'light';
    return m;
  }

  function apply(mode, persist) {
    const m = normalize(mode);
    const r = resolved(m);
    document.documentElement.dataset.theme = r;
    document.documentElement.dataset.themeMode = m;
    document.documentElement.style.colorScheme = r;
    const meta = document.querySelector('meta[name="theme-color"]');
    if (meta) meta.setAttribute('content', r === 'dark' ? '#0b0f14' : '#f6f8fb');
    if (persist !== false) {
      try { localStorage.setItem(KEY, m); } catch { /* private mode */ }
    }
  }

  window.NexusTheme = {
    apply,
    resolved,
    mode() {
      try { return normalize(localStorage.getItem(KEY)); } catch { return 'system'; }
    },
  };

  apply(window.NexusTheme.mode(), false);
  media.addEventListener('change', function () {
    if (window.NexusTheme.mode() === 'system') apply('system', false);
  });
})();
