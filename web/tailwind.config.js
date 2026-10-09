/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        brand: {
          50: '#eef4ff',
          100: '#d9e5ff',
          500: '#3b6fd4',
          600: '#2f5cb5',
          700: '#264a92',
        },
      },
    },
  },
  plugins: [],
}
