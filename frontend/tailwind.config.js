/** @type {import('tailwindcss').Config} */
module.exports = {
  content: [
    "./src/pages/**/*.{js,ts,jsx,tsx,mdx}",
    "./src/components/**/*.{js,ts,jsx,tsx,mdx}",
    "./src/app/**/*.{js,ts,jsx,tsx,mdx}",
  ],
  theme: {
    extend: {
      colors: {
        background: "#0b0f19", // Deep dark space color
        surface: "#131b2e",    // Dark slate card surface
        cardHover: "#1b253f",
        
        // Severity Accents (Neon SRE Palette)
        severity: {
          info: "#06b6d4",     // Vibrant Cyan
          warning: "#f59e0b",  // Bright Amber
          critical: "#f43f5e", // Rose Neon
          fatal: "#e11d48",    // Intense Crimson
          resolved: "#10b981", // Emerald Green
        }
      },
      fontFamily: {
        sans: ["var(--font-sans)", "Inter", "sans-serif"],
      },
      backgroundImage: {
        "gradient-radial": "radial-gradient(var(--tw-gradient-stops))",
      }
    },
  },
  plugins: [],
}
