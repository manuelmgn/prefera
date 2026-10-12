/** @type {import('tailwindcss').Config} */
// Configuraçom do Tailwind para o CSS precompilado.
// Cando se engadan clases novas nas plantillas ou en app.js, rexenerar o CSS:
//   .tools/tailwindcss build -i internal/server/static/css/input.css -c tailwind.config.js -o internal/server/static/css/app.css --minify
// (input.css contén parches personalizados — non o sobrescribas co output)
module.exports = {
    darkMode: 'class',
    content: [
        'internal/server/templates/**/*.html',
        'internal/server/static/js/*.js',
    ],
    theme: {
        extend: {
            fontFamily: {
                sans: ['Inter', 'SF Pro Display', 'SF Pro Text', 'system-ui', '-apple-system', 'sans-serif'],
            },
            colors: {
                gold: '#FFD700',
                silver: '#C0C0C0',
                bronze: '#CD7F32',
            },
        },
    },
    plugins: [],
};
