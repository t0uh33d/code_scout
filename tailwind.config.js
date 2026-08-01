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
        // Read off the Figma "final" section (326:1081), dashboard frames
        // 326:1244 and 326:1394. Neutral dark palette, not GitHub-dark.
        'cs-primary': '#078DEE',
        'cs-card': '#17181C',
        'cs-container': '#0D0D0F',
        'cs-border': '#202124',
        'cs-nav-border': '#17181B',
        'cs-input': '#151619',
        'cs-input-border': '#2B2B2E',
        'cs-text': '#E8EFF5',
        'cs-muted': '#B5B6B6',
        'cs-placeholder': '#606062',
        // The card's star is a blue-tinted grey, not the neutral muted grey.
        'cs-star': '#A8ADC2',
        // Figma exposes these two as variables rather than raw hex:
        // --text/button/default and --text/button/disabled.
        'cs-btn-text': '#EFF2FE',
        'cs-avatar-border': '#494F66',
        'cs-pager': '#02243C',
        // Destructive actions and inline validation errors.
        'cs-danger': '#F85149',
        'cs-success': '#3FB950',
      },
      fontFamily: {
        'sans': ['Plus Jakarta Sans', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        // Buttons and pagination numerals are Raleway in the design.
        'display': ['Raleway', 'Plus Jakarta Sans', 'sans-serif'],
        'mono': ['JetBrains Mono', 'ui-monospace', 'monospace'],
      },
    },
  },
  plugins: [],
};
