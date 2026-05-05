/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./templates/**/*.html"],
  theme: {
    extend: {
      colors: {
        dark: "#0a0a0a",
        glass: {
          DEFAULT: "rgba(255, 255, 255, 0.05)",
          border: "rgba(255, 255, 255, 0.1)",
        },
        accent: {
          neon: "#7c3aed",
          blue: "#0ea5e9",
          pink: "#ec4899",
        }
      },
    },
  },
  plugins: [],
}
