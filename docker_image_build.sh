cd webview

rm -rf dist

npm run build:prod

cd ..

docker build -t myobj .
