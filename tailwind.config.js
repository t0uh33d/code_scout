/** @type {import('tailwindcss').Config} */
module.exports = {
    content: [
      "./view/**/*.templ",
      "./view/**/*.html",
    ],
    theme: {
      extend: {
        colors: {
          'dashboard-bg': '#111214',
          'primary': '#2667FF',
        },
      },
    },
    plugins: [],
  };
  