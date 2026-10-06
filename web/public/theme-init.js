// Runs before the stylesheet: the saved choice is applied before first paint.
(() => {
  let theme = 'dark';
  let language='en';try{if(localStorage.getItem('svolo:language')==='it')language='it';}catch{}
  document.documentElement.lang=language;
  try { if (localStorage.getItem('svolo:theme') === 'light') theme = 'light'; } catch { /* Private storage unavailable: keep dark. */ }
  document.documentElement.dataset.theme = theme;
  document.documentElement.style.colorScheme = theme;
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme === 'dark' ? '#0c141d' : '#f8fafb');
})();
