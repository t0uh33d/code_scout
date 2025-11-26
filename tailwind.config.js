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
          'eerie-black' : '#1B1C1D',
          'arsenic' : '#414143',
        },
        fontFamily: {
          'sans': ['Plus Jakarta Sans', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        },
      },
    },
    plugins: [],
  };
  