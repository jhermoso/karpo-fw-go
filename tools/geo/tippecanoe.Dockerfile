# tippecanoe (felt, BSD-2) compilado desde una etiqueta fija: no hay imagen oficial pública.
FROM alpine:3.20 AS build
RUN apk add --no-cache build-base sqlite-dev zlib-dev bash git
RUN git clone --depth 1 --branch 2.79.0 https://github.com/felt/tippecanoe.git /src
WORKDIR /src
RUN make -j4 && make install
FROM alpine:3.20
RUN apk add --no-cache libstdc++ sqlite-libs zlib
COPY --from=build /usr/local/bin/tippecanoe* /usr/local/bin/tile-join /usr/local/bin/
ENTRYPOINT ["tippecanoe"]
