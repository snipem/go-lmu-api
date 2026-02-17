# go-lmu-api

Auto-generated Go client stubs for the Le Mans Ultimate (LMU) REST API.

## Warning

The struct inference is **naive**: it calls each GET endpoint once, looks at the JSON that comes back, and turns it into Go structs. It does not handle polymorphic responses, optional fields that happen to be absent, or type variations across different game states. **Expect it to break** when the API returns shapes that differ from what was seen during generation.

## Usage

### Generate stubs

Requires the game to be running (API at `localhost:6397`).

```
make generate
```

This will:
1. Fetch `/swagger-schema.json`
2. Generate client methods for all 179 endpoints
3. Call every parameterless GET endpoint and infer Go structs from live JSON responses
4. Write `lib/models.go` and `lib/client.go`

### Live standings TUI

```
make standings
./standings.exe
```

```
  LMU Live  |  PRACTICE1  |  20:12:03  |  24 cars

  P    #  Team             Driver                 Cls   PIC Laps      Gap      S1      S2      S3     Last     Best  Vmax Pit
─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  1   61  Iron Lynx        Lin Hodenius           GT3     1    7      ---   38.90   67.16   41.58 2:27.633 2:25.339   244   0
  2   81  TF Sport         Rui Andrade            GT3     2   14  +  1.09   39.67   67.55   41.38 2:28.600 2:26.427   247   0
  3   63  Iron Lynx        Brenton Grove          GT3     3   16  +  1.17   39.38   67.30   41.55 2:28.231 2:26.514   226   0
  4   46  Team WRT         Ahmad Al Harthy        GT3     4   15  +  1.29   39.63   69.07   41.80 2:30.497 2:26.630   223   0
  5   87  Akkodis ASP Team José María López       GT3     5   17  +  1.50   39.79   67.25   43.49 2:30.534 2:26.844   220   0
  6   60  Iron Lynx        Andrew Gilbert         GT3     6   18  +  1.67   39.50   68.58   41.14 2:29.219 2:27.013   250   0
  7   92  Manthey 1st Pho… Ryan Hardwick          GT3     7    8  +  1.85   39.99   66.64   40.72 2:27.347 2:27.185   252   0
  8   10  Racing Spirit o… Eduardo Barrichello    GT3     8   17  +  2.09   39.74   68.42   41.76 2:29.919 2:27.434   256   0
  9   13  AWA Racing       Matt Bell              GT3     9   15  +  2.24   40.19   67.94   41.32 2:29.450 2:27.579   247   0
 10   33  TF Sport         Jonny Edgar            GT3    10   17  +  2.25   40.23   70.02   42.91 2:33.161 2:27.591   223   0
 11   78  Akkodis ASP Team Finn Gehrsitz          GT3    11   17  +  2.53   39.87   68.17   42.05 2:30.083 2:27.866   252   0
 12   31  The Bend Team W… Timur Boguslavskiy     GT3    12   18  +  3.33   39.67   68.25   41.92 2:29.834 2:28.666   254   0
 13   27  Heart of Racing… Ian James              GT3    13   17  +  3.36   40.28   70.70   42.68 2:33.651 2:28.703   226   0
 14   90  Manthey          Antares Au             GT3    14   15  +  3.48   39.96   67.71   41.44 2:29.108 2:28.821   223   0
 15  193  Ziggo Sport Tem… Eddie Cheever          GT3    15   13  +  3.51   40.62   72.33   42.70 2:35.654 2:28.847   219   0
 16   57  Kessel Racing    Takeshi Kimura         GT3    16   12  +  3.81   40.14   69.26   42.32 2:31.724 2:29.148   255   0
 17   59  United Autospor… James Cottingham       GT3    17   18  +  4.16   40.13   70.12   41.92 2:32.171 2:29.499   246   0
 18   95  United Autospor… Sean Gelael            GT3    18   18  +  4.36   40.08   70.12   41.95 2:32.153 2:29.698   247   0
 19  150  Richard Mille A… Riccardo Agostini      GT3    19   13  +  4.51   40.33   69.93   42.73 2:32.990 2:29.852   254   0
 20   88  Proton Competit… Stefano Gattuso        GT3    20   11  +  5.74   40.31   69.42   42.42 2:32.154 2:31.079   246   0
 21   21  Vista AF Corse   François Heriau        GT3    21   17  +  6.00   40.25   69.65   42.40 2:32.302 2:31.335   254   0
 22   77  Proton Competit… Bernardo Sousa         GT3    22   15  +  6.04   40.45   70.55   42.71 2:33.713 2:31.375   220   0
 23   54  Vista AF Corse   Francesco Castellacci  GT3    23   17  +  6.07   40.41   70.39   43.49 2:34.293 2:31.412   222   0
```

### Makefile targets

| Target | Description |
|---|---|
| `make generate` | Regenerate `lib/` from live API |
| `make build` | Generate + compile lib |
| `make standings` | Build the standings TUI |
| `make clean` | Remove generated files |
