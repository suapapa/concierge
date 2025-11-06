# Concierge

임시로 파일을 퍼블릭하게 서빙하기 위에 잠시 맡겨두기 위한 용도의 웹앱.

파일 맡기기:
```sh
curl -X POST http://localhost:8080/save \
  -F "file=@example.txt" \
  -F "mime=text/plain" \
  -F "ttl=5"
```