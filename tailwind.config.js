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
          'blue-cola':'#078DEE',
        },
        fontFamily: {
          'sans': ['Plus Jakarta Sans', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        },
      },
    },
    plugins: [],
  };
  