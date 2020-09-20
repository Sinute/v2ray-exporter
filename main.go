package main

import (
    "context"
    "crypto/md5"
    "flag"
    "fmt"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
    "log"
    "net/http"
    "os"
    "strings"
    "time"

    "github.com/prometheus/client_golang/prometheus/promhttp"
    "github.com/v2ray/v2ray-core/app/stats/command"
    grpc "google.golang.org/grpc"
)

var addr = flag.String("web.listen-address", ":9333", "Address on which to expose metrics and web interface.")
var v2rayIP = flag.String("v2ray.server-ip", "127.0.0.1", "V2ray server ip.")
var v2rayPort = flag.Int("v2ray.server-port", 10085, "V2ray server port.")

type Traffic struct {
    User    map[string]*link
    Inbound map[string]*link
}

type link struct {
    LastUp   int64
    LastDown int64
    Up       int64
    Down     int64
}

func main() {
    flag.Parse()
    go recordMetrics(*v2rayIP, uint16(*v2rayPort))
    http.Handle("/metrics", promhttp.Handler())
    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte(`<html>
			<head><title>V2Ray Exporter</title></head>
			<body>
			<h1>V2Ray Exporter</h1>
			<p><a href="/metrics">Metrics</a></p>
			</body>
			</html>`))
    })
    log.Printf("Start at %s", *addr)
    log.Fatal(http.ListenAndServe(*addr, nil))
}

func recordMetrics(ip string, port uint16) {
    hostName, _ := os.Hostname()
    userTrafficCounter := make(map[string]prometheus.Counter)
    traffic := &Traffic{
        User:    make(map[string]*link),
        Inbound: make(map[string]*link),
    }
    for {
        err := v2traffic(fmt.Sprintf("%s:%d", ip, port), traffic)
        if err != nil {
            log.Println(fmt.Sprintf("[ERROR] %v", err))
        }
        for n, u := range traffic.User {
            up := int64(0)
            down := int64(0)
            if u.LastUp != 0 && u.Up > u.LastUp {
                up = u.Up - u.LastUp
            }
            if u.LastDown != 0 && u.Down > u.LastDown {
                down = u.Down - u.LastDown
            }
            bUpName := md5.Sum([]byte(n + "_up"))
            upName := string(bUpName[:])
            bDownName := md5.Sum([]byte(n + "_down"))
            downName := string(bDownName[:])
            if _, ok := userTrafficCounter[upName]; !ok {
                labels := make(prometheus.Labels)
                labels["type"] = "user"
                labels["name"] = n
                labels["traffic"] = "up"
                labels["host"] = fmt.Sprintf("%s:%d", hostName, port)
                userTrafficCounter[upName] = promauto.NewCounter(prometheus.CounterOpts{
                    Namespace:   "user",
                    Subsystem:   "traffic",
                    Name:        "up",
                    Help:        "up",
                    ConstLabels: labels,
                })
            }
            if _, ok := userTrafficCounter[downName]; !ok {
                labels := make(prometheus.Labels)
                labels["type"] = "user"
                labels["name"] = n
                labels["traffic"] = "down"
                labels["host"] = fmt.Sprintf("%s:%d", hostName, port)
                userTrafficCounter[downName] = promauto.NewCounter(prometheus.CounterOpts{
                    Namespace:   "user",
                    Subsystem:   "traffic",
                    Name:        "down",
                    Help:        "down",
                    ConstLabels: labels,
                })
            }
            userTrafficCounter[upName].Add(float64(up))
            userTrafficCounter[downName].Add(float64(down))
            u.LastUp = u.Up
            u.LastDown = u.Down
        }
        time.Sleep(5 * time.Second)
    }
}

func v2traffic(addr string, traffic *Traffic) (err error) {
    ctx := context.Background()
    ctx, _ = context.WithTimeout(ctx, time.Second)
    cmdConn, err := grpc.DialContext(ctx, addr, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithDisableRetry())
    if err != nil {
        return err
    }
    defer cmdConn.Close()
    client := command.NewStatsServiceClient(cmdConn)
    rsp, err := client.QueryStats(context.TODO(), &command.QueryStatsRequest{
        Pattern: "",
        Reset_:  false,
    })
    if err != nil {
        return err
    }
    for _, v := range rsp.Stat {
        names := strings.Split(v.Name, ">>>")
        if len(names) != 4 {
            return fmt.Errorf("[ERROR] Unrecognized Name: %s", v.Name)
        }
        switch names[0] {
        case "user":
            if _, ok := traffic.User[names[1]]; !ok {
                traffic.User[names[1]] = &link{
                    LastUp:   0,
                    LastDown: 0,
                    Up:       0,
                    Down:     0,
                }
            }
            switch names[3] {
            case "uplink":
                traffic.User[names[1]].Up = v.Value
            case "downlink":
                traffic.User[names[1]].Down = v.Value
            default:
            }
        case "inbound":
            if _, ok := traffic.Inbound[names[1]]; !ok {
                traffic.Inbound[names[1]] = &link{
                    Up:   0,
                    Down: 0,
                }
            }
            switch names[3] {
            case "uplink":
                traffic.Inbound[names[1]].Up = v.Value
            case "downlink":
                traffic.Inbound[names[1]].Down = v.Value
            default:
                log.Println(fmt.Sprintf("[WARNING] Unrecognized Type: %s", v.Name))
            }
        default:
            return fmt.Errorf("[ERROR] Unrecognized Name: %s", v.Name)
        }
    }
    return nil
}
