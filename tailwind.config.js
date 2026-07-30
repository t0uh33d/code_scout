/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    "./view/**/*.templ",
    "./view/**/*.html",
  ],
  theme: {
    extend: {
      colors: {
        'chinese-black': '#111214',
        'primary-crayola': '#2667FF',
        'fnt-platinum': '#E7E7E8',
        'blue-cola': '#078DEE',
        'eerie-black': '#1B1C1D',
        'arsenic': '#414143',
        // Values sampled from the Figma "final" section (326:1081) —
        // neutral dark palette, not GitHub-dark.
        'cs-primary': '#078DEE',
        'cs-card': '#17181C',
        'cs-container': '#0D0D0F',
        'cs-muted': '#8B949E',
        'cs-border': '#21262D',
        'cs-input': '#151619',
        'cs-input-border': '#2B2B2E',
      },
      fontFamily: {
        'sans': ['Plus Jakarta Sans', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        'mono': ['JetBrains Mono', 'ui-monospace', 'monospace'],
      },
    },
  },
  plugins: [],
};
