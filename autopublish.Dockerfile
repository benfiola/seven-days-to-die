FROM docker.io/golang:1.23 AS autopublish

WORKDIR /app

ADD go.mod go.mod
ADD go.sum go.sum
ADD cmd cmd

RUN go build -o /autopublish cmd/autopublish/main.go


FROM cm2network/steamcmd
ENV PATH="/home/steam/steamcmd:${PATH}"
RUN ln -s /home/steam/steamcmd/steamcmd.sh /home/steam/steamcmd/steamcmd
COPY --from=autopublish /autopublish /autopublish
CMD ["/autopublish"]
