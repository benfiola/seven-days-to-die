FROM docker.io/golang:1.23 AS autopublish

WORKDIR /app

ADD go.mod go.mod
ADD go.sum go.sum
ADD cmd cmd

RUN go build -o /autopublish cmd/autopublish/main.go


FROM cm2network/steamcmd
RUN ln -s /home/steam/steamcmd.sh /usr/local/bin/steamcmd
COPY --from=autopublish /autopublish /autopublish
CMD ["/autopublish"]
