package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jhump/protoreflect/desc/protoparse"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	_ "modernc.org/sqlite"
)

type Pet struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Plant struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Dinosaur struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Car struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Movie struct {
	ID       int64   `json:"ID"`
	Title    string  `json:"Title"`
	Year     int     `json:"Year"`
	Genre    string  `json:"Genre"`
	Rating   float64 `json:"Rating"`
	Director string  `json:"Director"`
}

type Server struct {
	store *Store
}

func main() {
	store, err := NewStore()
	if err != nil {
		log.Fatalf("db init failed: %v", err)
	}

	srv := &Server{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("/openapi/openapi.json", srv.handleOpenAPI)
	mux.HandleFunc("/swagger/swagger.json", srv.handleSwagger2)
	mux.HandleFunc("/wdsl/wsdl", srv.handleWSDL)
	mux.HandleFunc("/graphql", srv.handleGraphQL)
	mux.HandleFunc("/graphql/schema", srv.handleGraphQLSchema)
	mux.HandleFunc("/openapi/pets", srv.handlePets)
	mux.HandleFunc("/openapi/pets/", srv.handlePet)
	mux.HandleFunc("/swagger/dinosaurs", srv.handleDinosaurs)
	mux.HandleFunc("/swagger/dinosaurs/", srv.handleDinosaur)
	mux.HandleFunc("/wdsl/soap", srv.handleSOAP)
	mux.HandleFunc("/odata/", srv.handleOData)
	mux.HandleFunc("/jsonrpc", srv.handleJSONRPC)
	mux.HandleFunc("/jsonrpc/openrpc.json", srv.handleOpenRPCSpec)

	if err := startGRPCClothesMocks(); err != nil {
		log.Printf("warning: grpc mock init failed (continuing without gRPC): %v", err)
	}

	log.Println("mock server listening on :9999 (OpenAPI /openapi + Swagger2 /swagger + WSDL /wdsl + GraphQL /graphql + OData /odata + JSON-RPC /jsonrpc + gRPC clothes)")
	if err := http.ListenAndServe(":9999", mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(openAPISpec))
}

func (s *Server) handleSwagger2(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(swagger2Spec))
}

func (s *Server) handleWSDL(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(wsdlSpec))
}

func (s *Server) handleGraphQLSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(graphQLSDL))
}

func (s *Server) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	query := strings.ToLower(payload.Query)
	if strings.TrimSpace(query) == "" {
		respondGraphQLError(w, "missing query")
		return
	}

	switch {
	case strings.Contains(query, "listcars"):
		limit := 2
		if val, ok := graphqlVarInt(payload.Variables, "limit"); ok {
			limit = val
		}
		cars, err := s.store.ListCars(r.Context(), limit)
		if err != nil {
			respondGraphQLError(w, "db error")
			return
		}
		respondGraphQL(w, map[string]any{"listCars": cars})
	case strings.Contains(query, "getcar"):
		idStr, ok := graphqlVarString(payload.Variables, "id")
		if !ok || strings.TrimSpace(idStr) == "" {
			respondGraphQLError(w, "missing id")
			return
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			respondGraphQLError(w, "invalid id")
			return
		}
		car, err := s.store.GetCar(r.Context(), id)
		if err == sql.ErrNoRows {
			respondGraphQL(w, map[string]any{"getCar": nil})
			return
		}
		if err != nil {
			respondGraphQLError(w, "db error")
			return
		}
		respondGraphQL(w, map[string]any{"getCar": car})
	case strings.Contains(query, "createcar"):
		name, ok := graphqlVarString(payload.Variables, "name")
		if !ok || strings.TrimSpace(name) == "" {
			respondGraphQLError(w, "missing name")
			return
		}
		car, err := s.store.CreateCar(r.Context(), name)
		if err != nil {
			respondGraphQLError(w, "db error")
			return
		}
		respondGraphQL(w, map[string]any{"createCar": car})
	default:
		respondGraphQLError(w, "unknown operation")
	}
}

func (s *Server) handlePets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		limit := 2
		if val := r.URL.Query().Get("limit"); val != "" {
			if parsed, err := strconv.Atoi(val); err == nil {
				limit = parsed
			}
		}
		pets, err := s.store.ListPets(r.Context(), limit)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		respondJSON(w, http.StatusOK, pets)
	case http.MethodPost:
		var input struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		pet, err := s.store.CreatePet(r.Context(), input.Name)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		respondJSON(w, http.StatusCreated, pet)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePet(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/openapi/pets/")
	if idStr == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		pet, err := s.store.GetPet(r.Context(), id)
		if err == sql.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		respondJSON(w, http.StatusOK, pet)
	case http.MethodPut:
		var input struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		pet, err := s.store.UpdatePet(r.Context(), id, input.Name)
		if err == sql.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		respondJSON(w, http.StatusOK, pet)
	case http.MethodDelete:
		if err := s.store.DeletePet(r.Context(), id); err == sql.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDinosaurs(w http.ResponseWriter, r *http.Request) {
	if !authorizedDinosaur(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit := 2
		if val := r.URL.Query().Get("limit"); val != "" {
			if parsed, err := strconv.Atoi(val); err == nil {
				limit = parsed
			}
		}
		dinos, err := s.store.ListDinosaurs(r.Context(), limit)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		respondJSON(w, http.StatusOK, dinos)
	case http.MethodPost:
		var input struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		dino, err := s.store.CreateDinosaur(r.Context(), input.Name)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		respondJSON(w, http.StatusCreated, dino)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleDinosaur(w http.ResponseWriter, r *http.Request) {
	if !authorizedDinosaur(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/swagger/dinosaurs/")
	if idStr == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		dino, err := s.store.GetDinosaur(r.Context(), id)
		if err == sql.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		respondJSON(w, http.StatusOK, dino)
	case http.MethodPut:
		var input struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		dino, err := s.store.UpdateDinosaur(r.Context(), id, input.Name)
		if err == sql.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		respondJSON(w, http.StatusOK, dino)
	case http.MethodDelete:
		if err := s.store.DeleteDinosaur(r.Context(), id); err == sql.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func authorizedDinosaur(r *http.Request) bool {
	expected := strings.TrimSpace(os.Getenv("DINOSAURS_SWAGGER2_TOKEN"))
	if expected == "" {
		expected = strings.TrimSpace(os.Getenv("DINOSAUR_TOKEN"))
	}
	if expected == "" {
		expected = "MOCK_DINO_TOKEN"
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return false
	}
	if len(auth) < len("Bearer ")+1 {
		return false
	}
	if !strings.EqualFold(auth[:7], "Bearer ") {
		return false
	}
	token := strings.TrimSpace(auth[7:])
	if token == "" {
		return false
	}
	if token == expected || token == "MOCK_DINO_TOKEN" {
		return true
	}
	for _, part := range strings.Split(expected, ",") {
		if strings.TrimSpace(part) == token {
			return true
		}
	}
	return false
}

func (s *Server) handleSOAP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !authorizedSOAP(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	opName, params, err := parseSOAPRequest(r.Body)
	if err != nil {
		writeSOAPFault(w, http.StatusBadRequest, "invalid SOAP request")
		return
	}

	switch opName {
	case "ListPlants":
		plants, err := s.store.ListPlants(r.Context(), 0)
		if err != nil {
			writeSOAPFault(w, http.StatusInternalServerError, "db error")
			return
		}
		writeSOAPResponse(w, listPlantsXML(plants))
	case "GetPlant":
		id, err := parseID(params, "id")
		if err != nil {
			writeSOAPFault(w, http.StatusBadRequest, "missing id")
			return
		}
		plant, err := s.store.GetPlant(r.Context(), id)
		if err == sql.ErrNoRows {
			writeSOAPFault(w, http.StatusNotFound, "not found")
			return
		}
		if err != nil {
			writeSOAPFault(w, http.StatusInternalServerError, "db error")
			return
		}
		writeSOAPResponse(w, plantResponseXML("GetPlantResponse", plant))
	case "CreatePlant":
		name := strings.TrimSpace(params["name"])
		if name == "" {
			writeSOAPFault(w, http.StatusBadRequest, "missing name")
			return
		}
		plant, err := s.store.CreatePlant(r.Context(), name)
		if err != nil {
			writeSOAPFault(w, http.StatusInternalServerError, "db error")
			return
		}
		writeSOAPResponse(w, plantResponseXML("CreatePlantResponse", plant))
	case "UpdatePlant":
		id, err := parseID(params, "id")
		if err != nil {
			writeSOAPFault(w, http.StatusBadRequest, "missing id")
			return
		}
		name := strings.TrimSpace(params["name"])
		if name == "" {
			writeSOAPFault(w, http.StatusBadRequest, "missing name")
			return
		}
		plant, err := s.store.UpdatePlant(r.Context(), id, name)
		if err == sql.ErrNoRows {
			writeSOAPFault(w, http.StatusNotFound, "not found")
			return
		}
		if err != nil {
			writeSOAPFault(w, http.StatusInternalServerError, "db error")
			return
		}
		writeSOAPResponse(w, plantResponseXML("UpdatePlantResponse", plant))
	case "DeletePlant":
		id, err := parseID(params, "id")
		if err != nil {
			writeSOAPFault(w, http.StatusBadRequest, "missing id")
			return
		}
		if err := s.store.DeletePlant(r.Context(), id); err == sql.ErrNoRows {
			writeSOAPFault(w, http.StatusNotFound, "not found")
			return
		} else if err != nil {
			writeSOAPFault(w, http.StatusInternalServerError, "db error")
			return
		}
		writeSOAPResponse(w, `<p:DeletePlantResponse xmlns:p="http://example.com/plants"><success>true</success></p:DeletePlantResponse>`)
	default:
		writeSOAPFault(w, http.StatusBadRequest, "unknown operation")
	}
}

func authorizedSOAP(r *http.Request) bool {
	expected := strings.TrimSpace(os.Getenv("PLANTS_WSDL_TOKEN"))
	if expected == "" {
		expected = strings.TrimSpace(os.Getenv("MOCKSOAP_TOKEN"))
	}
	if expected == "" {
		expected = "MOCK_TOKEN"
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return false
	}
	if len(auth) < len("Bearer ")+1 {
		return false
	}
	if !strings.EqualFold(auth[:7], "Bearer ") {
		return false
	}
	token := strings.TrimSpace(auth[7:])
	if token == "" {
		return false
	}
	if token == expected || token == "MOCK_TOKEN" {
		return true
	}
	for _, part := range strings.Split(expected, ",") {
		if strings.TrimSpace(part) == token {
			return true
		}
	}
	return false
}

func authorizedGraphQL(r *http.Request) bool {
	user, pass, ok := r.BasicAuth()
	if !ok {
		return false
	}
	expectedUser := strings.TrimSpace(os.Getenv("GRAPHQL_USERNAME"))
	expectedPass := strings.TrimSpace(os.Getenv("GRAPHQL_PASSWORD"))
	if expectedUser == "" {
		expectedUser = "graphql-user"
	}
	if expectedPass == "" {
		expectedPass = "MOCK_GRAPHQL_PASS"
	}
	return user == expectedUser && pass == expectedPass
}

func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func respondGraphQL(w http.ResponseWriter, data map[string]any) {
	respondJSON(w, http.StatusOK, map[string]any{"data": data})
}

func respondGraphQLError(w http.ResponseWriter, message string) {
	respondJSON(w, http.StatusOK, map[string]any{
		"errors": []map[string]string{{"message": message}},
	})
}

func graphqlVarInt(vars map[string]any, key string) (int, bool) {
	if vars == nil {
		return 0, false
	}
	val, ok := vars[key]
	if !ok {
		return 0, false
	}
	switch v := val.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case string:
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func graphqlVarString(vars map[string]any, key string) (string, bool) {
	if vars == nil {
		return "", false
	}
	val, ok := vars[key]
	if !ok {
		return "", false
	}
	switch v := val.(type) {
	case string:
		return v, true
	case fmt.Stringer:
		return v.String(), true
	case float64:
		return strconv.FormatInt(int64(v), 10), true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	default:
		return "", false
	}
}

func parseSOAPRequest(r io.Reader) (string, map[string]string, error) {
	decoder := xml.NewDecoder(r)
	inBody := false
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "Body" {
				inBody = true
				continue
			}
			if inBody {
				rawName := t.Name.Local
				opName := normalizeSOAPOperationName(rawName)
				params := map[string]string{}
				for {
					inner, err := decoder.Token()
					if err != nil {
						return "", nil, err
					}
					switch it := inner.(type) {
					case xml.StartElement:
						var val string
						if err := decoder.DecodeElement(&val, &it); err != nil {
							return "", nil, err
						}
						params[it.Name.Local] = strings.TrimSpace(val)
					case xml.EndElement:
						if it.Name.Local == rawName {
							return opName, params, nil
						}
					}
				}
			}
		}
	}
	return "", nil, fmt.Errorf("soap: operation not found")
}

// gRPC clothes mocks (unary for now).

type grpcClothesItem struct {
	ID   string
	Name string
}

type grpcClothesService interface {
	ListClothes(context.Context, *dynamicpb.Message) (*dynamicpb.Message, error)
}

type grpcClothesServer struct {
	category string
	items    []grpcClothesItem

	reqLimitField protoreflect.FieldDescriptor
	reqIDField    protoreflect.FieldDescriptor

	respDesc        protoreflect.MessageDescriptor
	respItemsField  protoreflect.FieldDescriptor
	respServerField protoreflect.FieldDescriptor

	itemDesc          protoreflect.MessageDescriptor
	itemIDField       protoreflect.FieldDescriptor
	itemNameField     protoreflect.FieldDescriptor
	itemCategoryField protoreflect.FieldDescriptor
}

type protoRefs struct {
	serviceDesc protoreflect.ServiceDescriptor
	methodDesc  protoreflect.MethodDescriptor
}

func startGRPCClothesMocks() error {
	basePort := envInt("GRPC_BASE_PORT", 50051)
	refs, err := loadClothesProto("clothes.proto")
	if err != nil {
		return err
	}

	servers := []struct {
		category string
		port     int
		items    []grpcClothesItem
	}{
		{category: "hats", port: basePort, items: []grpcClothesItem{{ID: "1", Name: "beanie"}, {ID: "2", Name: "fedora"}, {ID: "3", Name: "cap"}}},
		{category: "shoes", port: basePort + 1, items: []grpcClothesItem{{ID: "1", Name: "sneakers"}, {ID: "2", Name: "boots"}, {ID: "3", Name: "loafers"}}},
		{category: "pants", port: basePort + 2, items: []grpcClothesItem{{ID: "1", Name: "jeans"}, {ID: "2", Name: "chinos"}, {ID: "3", Name: "shorts"}}},
		{category: "shirts", port: basePort + 3, items: []grpcClothesItem{{ID: "1", Name: "tee"}, {ID: "2", Name: "oxford"}, {ID: "3", Name: "hoodie"}}},
	}

	for _, cfg := range servers {
		server, err := newGRPCClothesServer(cfg.category, cfg.items, refs.methodDesc)
		if err != nil {
			return err
		}
		serviceDesc := buildGRPCServiceDesc(refs.serviceDesc, refs.methodDesc)
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.port))
		if err != nil {
			return err
		}
		grpcServer := grpc.NewServer()
		grpcServer.RegisterService(serviceDesc, server)
		reflection.Register(grpcServer)
		go func(category string, port int, lis net.Listener) {
			log.Printf("grpc clothes mock (%s) listening on :%d", category, port)
			if err := grpcServer.Serve(lis); err != nil {
				log.Printf("grpc clothes mock (%s) error: %v", category, err)
			}
		}(cfg.category, cfg.port, listener)
	}
	return nil
}

func loadClothesProto(path string) (protoRefs, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return protoRefs{}, err
	}
	parser := protoparse.Parser{ImportPaths: []string{filepath.Dir(absPath)}}
	files, err := parser.ParseFiles(filepath.Base(absPath))
	if err != nil {
		return protoRefs{}, err
	}
	if len(files) == 0 {
		return protoRefs{}, fmt.Errorf("no proto files parsed")
	}

	fileDesc, err := protodesc.NewFile(files[0].AsFileDescriptorProto(), nil)
	if err != nil {
		return protoRefs{}, err
	}

	service := fileDesc.Services().ByName("ClothesService")
	if service == nil {
		return protoRefs{}, fmt.Errorf("service ClothesService not found")
	}
	method := service.Methods().ByName("ListClothes")
	if method == nil {
		return protoRefs{}, fmt.Errorf("method ListClothes not found")
	}
	return protoRefs{serviceDesc: service, methodDesc: method}, nil
}

func newGRPCClothesServer(category string, items []grpcClothesItem, method protoreflect.MethodDescriptor) (*grpcClothesServer, error) {
	reqDesc := method.Input()
	respDesc := method.Output()

	reqLimit := reqDesc.Fields().ByName("limit")
	reqID := reqDesc.Fields().ByName("id")
	if reqLimit == nil || reqID == nil {
		return nil, fmt.Errorf("request fields limit/id missing in proto")
	}

	itemsField := respDesc.Fields().ByName("items")
	serverField := respDesc.Fields().ByName("server")
	if itemsField == nil || serverField == nil {
		return nil, fmt.Errorf("response fields items/server missing in proto")
	}
	itemDesc := itemsField.Message()
	if itemDesc == nil {
		return nil, fmt.Errorf("items field is missing message type")
	}
	itemID := itemDesc.Fields().ByName("id")
	itemName := itemDesc.Fields().ByName("name")
	itemCategory := itemDesc.Fields().ByName("category")
	if itemID == nil || itemName == nil || itemCategory == nil {
		return nil, fmt.Errorf("item fields id/name/category missing in proto")
	}

	return &grpcClothesServer{
		category:          category,
		items:             items,
		reqLimitField:     reqLimit,
		reqIDField:        reqID,
		respDesc:          respDesc,
		respItemsField:    itemsField,
		respServerField:   serverField,
		itemDesc:          itemDesc,
		itemIDField:       itemID,
		itemNameField:     itemName,
		itemCategoryField: itemCategory,
	}, nil
}

func buildGRPCServiceDesc(service protoreflect.ServiceDescriptor, method protoreflect.MethodDescriptor) *grpc.ServiceDesc {
	return &grpc.ServiceDesc{
		ServiceName: string(service.FullName()),
		HandlerType: (*grpcClothesService)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: string(method.Name()),
				Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
					req := dynamicpb.NewMessage(method.Input())
					if err := dec(req); err != nil {
						return nil, err
					}
					if interceptor == nil {
						return srv.(grpcClothesService).ListClothes(ctx, req)
					}
					info := &grpc.UnaryServerInfo{
						Server:     srv,
						FullMethod: fmt.Sprintf("/%s/%s", service.FullName(), method.Name()),
					}
					handler := func(ctx context.Context, req any) (any, error) {
						return srv.(grpcClothesService).ListClothes(ctx, req.(*dynamicpb.Message))
					}
					return interceptor(ctx, req, info, handler)
				},
			},
		},
	}
}

func (s *grpcClothesServer) ListClothes(ctx context.Context, req *dynamicpb.Message) (*dynamicpb.Message, error) {
	_ = ctx
	id := grpcGetStringField(req, s.reqIDField)
	limit := int(grpcGetIntField(req, s.reqLimitField))

	var selected []grpcClothesItem
	if id != "" {
		for _, item := range s.items {
			if item.ID == id {
				selected = append(selected, item)
				break
			}
		}
	} else {
		selected = append(selected, s.items...)
		if limit > 0 && limit < len(selected) {
			selected = selected[:limit]
		}
	}

	resp := dynamicpb.NewMessage(s.respDesc)
	resp.Set(s.respServerField, protoreflect.ValueOfString(s.category))
	list := resp.Mutable(s.respItemsField).List()
	for _, item := range selected {
		msg := dynamicpb.NewMessage(s.itemDesc)
		msg.Set(s.itemIDField, protoreflect.ValueOfString(item.ID))
		msg.Set(s.itemNameField, protoreflect.ValueOfString(item.Name))
		msg.Set(s.itemCategoryField, protoreflect.ValueOfString(s.category))
		list.Append(protoreflect.ValueOfMessage(msg))
	}
	return resp, nil
}

func grpcGetStringField(msg *dynamicpb.Message, field protoreflect.FieldDescriptor) string {
	if field == nil {
		return ""
	}
	return msg.Get(field).String()
}

func grpcGetIntField(msg *dynamicpb.Message, field protoreflect.FieldDescriptor) int64 {
	if field == nil {
		return 0
	}
	return msg.Get(field).Int()
}

func envInt(key string, fallback int) int {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return parsed
}

func normalizeSOAPOperationName(name string) string {
	if strings.HasSuffix(name, "Request") && len(name) > len("Request") {
		return strings.TrimSuffix(name, "Request")
	}
	return name
}

func writeSOAPResponse(w http.ResponseWriter, payload string) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(soapEnvelopeStart + payload + soapEnvelopeEnd))
}

func writeSOAPFault(w http.ResponseWriter, status int, message string) {
	payload := fmt.Sprintf(`<soapenv:Fault><faultcode>soapenv:Client</faultcode><faultstring>%s</faultstring></soapenv:Fault>`, escapeXML(message))
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(soapEnvelopeStart + payload + soapEnvelopeEnd))
}

func escapeXML(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

func parseID(params map[string]string, key string) (int64, error) {
	val := strings.TrimSpace(params[key])
	if val == "" {
		return 0, fmt.Errorf("missing id")
	}
	return strconv.ParseInt(val, 10, 64)
}

func listPlantsXML(plants []Plant) string {
	var b strings.Builder
	b.WriteString(`<p:ListPlantsResponse xmlns:p="http://example.com/plants"><plants>`)
	for _, plant := range plants {
		b.WriteString("<plant>")
		b.WriteString("<id>")
		b.WriteString(escapeXML(plant.ID))
		b.WriteString("</id>")
		b.WriteString("<name>")
		b.WriteString(escapeXML(plant.Name))
		b.WriteString("</name>")
		b.WriteString("</plant>")
	}
	b.WriteString("</plants></p:ListPlantsResponse>")
	return b.String()
}

func plantResponseXML(action string, plant Plant) string {
	return fmt.Sprintf(`<p:%s xmlns:p="http://example.com/plants"><plant><id>%s</id><name>%s</name></plant></p:%s>`, action, escapeXML(plant.ID), escapeXML(plant.Name), action)
}

const soapEnvelopeStart = `<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
<soapenv:Body>`

const soapEnvelopeEnd = `</soapenv:Body>
</soapenv:Envelope>`

// Store

type Store struct {
	db *sql.DB
}

type dbItem struct {
	ID   int64
	Name string
}

func NewStore() (*Store, error) {
	db, err := sql.Open("sqlite", "file:mock.db?mode=memory&cache=shared")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS pets (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS plants (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS dinosaurs (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS cars (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS movies (id INTEGER PRIMARY KEY AUTOINCREMENT, title TEXT NOT NULL, year INTEGER NOT NULL, genre TEXT NOT NULL, rating REAL NOT NULL, director TEXT NOT NULL)`); err != nil {
		return nil, err
	}
	if err := seedMovies(ctx, db); err != nil {
		return nil, err
	}
	if err := seedTable(ctx, db, "pets", []string{"pet-1", "pet-2", "pet-3"}); err != nil {
		return nil, err
	}
	if err := seedTable(ctx, db, "plants", []string{"fern", "cactus"}); err != nil {
		return nil, err
	}
	if err := seedTable(ctx, db, "dinosaurs", []string{"t-rex", "triceratops"}); err != nil {
		return nil, err
	}
	if err := seedTable(ctx, db, "cars", []string{"sedan", "truck"}); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func seedTable(ctx context.Context, db *sql.DB, table string, names []string) error {
	if _, err := allowTable(table); err != nil {
		return err
	}
	var count int
	row := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table))
	if err := row.Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	for _, name := range names {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (name) VALUES (?)", table), name); err != nil {
			return err
		}
	}
	return nil
}

func allowTable(table string) (string, error) {
	switch table {
	case "pets", "plants", "dinosaurs", "cars", "movies":
		return table, nil
	default:
		return "", fmt.Errorf("invalid table")
	}
}

func (s *Store) ListPets(ctx context.Context, limit int) ([]Pet, error) {
	items, err := s.listItems(ctx, "pets", limit)
	if err != nil {
		return nil, err
	}
	pets := make([]Pet, 0, len(items))
	for _, item := range items {
		pets = append(pets, Pet{ID: strconv.FormatInt(item.ID, 10), Name: item.Name})
	}
	return pets, nil
}

func (s *Store) GetPet(ctx context.Context, id int64) (Pet, error) {
	item, err := s.getItem(ctx, "pets", id)
	if err != nil {
		return Pet{}, err
	}
	return Pet{ID: strconv.FormatInt(item.ID, 10), Name: item.Name}, nil
}

func (s *Store) CreatePet(ctx context.Context, name string) (Pet, error) {
	item, err := s.createItem(ctx, "pets", name)
	if err != nil {
		return Pet{}, err
	}
	return Pet{ID: strconv.FormatInt(item.ID, 10), Name: item.Name}, nil
}

func (s *Store) UpdatePet(ctx context.Context, id int64, name string) (Pet, error) {
	item, err := s.updateItem(ctx, "pets", id, name)
	if err != nil {
		return Pet{}, err
	}
	return Pet{ID: strconv.FormatInt(item.ID, 10), Name: item.Name}, nil
}

func (s *Store) DeletePet(ctx context.Context, id int64) error {
	return s.deleteItem(ctx, "pets", id)
}

func (s *Store) ListPlants(ctx context.Context, limit int) ([]Plant, error) {
	items, err := s.listItems(ctx, "plants", limit)
	if err != nil {
		return nil, err
	}
	plants := make([]Plant, 0, len(items))
	for _, item := range items {
		plants = append(plants, Plant{ID: strconv.FormatInt(item.ID, 10), Name: item.Name})
	}
	return plants, nil
}

func (s *Store) GetPlant(ctx context.Context, id int64) (Plant, error) {
	item, err := s.getItem(ctx, "plants", id)
	if err != nil {
		return Plant{}, err
	}
	return Plant{ID: strconv.FormatInt(item.ID, 10), Name: item.Name}, nil
}

func (s *Store) CreatePlant(ctx context.Context, name string) (Plant, error) {
	item, err := s.createItem(ctx, "plants", name)
	if err != nil {
		return Plant{}, err
	}
	return Plant{ID: strconv.FormatInt(item.ID, 10), Name: item.Name}, nil
}

func (s *Store) UpdatePlant(ctx context.Context, id int64, name string) (Plant, error) {
	item, err := s.updateItem(ctx, "plants", id, name)
	if err != nil {
		return Plant{}, err
	}
	return Plant{ID: strconv.FormatInt(item.ID, 10), Name: item.Name}, nil
}

func (s *Store) DeletePlant(ctx context.Context, id int64) error {
	return s.deleteItem(ctx, "plants", id)
}

func (s *Store) ListDinosaurs(ctx context.Context, limit int) ([]Dinosaur, error) {
	items, err := s.listItems(ctx, "dinosaurs", limit)
	if err != nil {
		return nil, err
	}
	dinos := make([]Dinosaur, 0, len(items))
	for _, item := range items {
		dinos = append(dinos, Dinosaur{ID: strconv.FormatInt(item.ID, 10), Name: item.Name})
	}
	return dinos, nil
}

func (s *Store) GetDinosaur(ctx context.Context, id int64) (Dinosaur, error) {
	item, err := s.getItem(ctx, "dinosaurs", id)
	if err != nil {
		return Dinosaur{}, err
	}
	return Dinosaur{ID: strconv.FormatInt(item.ID, 10), Name: item.Name}, nil
}

func (s *Store) CreateDinosaur(ctx context.Context, name string) (Dinosaur, error) {
	item, err := s.createItem(ctx, "dinosaurs", name)
	if err != nil {
		return Dinosaur{}, err
	}
	return Dinosaur{ID: strconv.FormatInt(item.ID, 10), Name: item.Name}, nil
}

func (s *Store) UpdateDinosaur(ctx context.Context, id int64, name string) (Dinosaur, error) {
	item, err := s.updateItem(ctx, "dinosaurs", id, name)
	if err != nil {
		return Dinosaur{}, err
	}
	return Dinosaur{ID: strconv.FormatInt(item.ID, 10), Name: item.Name}, nil
}

func (s *Store) DeleteDinosaur(ctx context.Context, id int64) error {
	return s.deleteItem(ctx, "dinosaurs", id)
}

func (s *Store) ListCars(ctx context.Context, limit int) ([]Car, error) {
	items, err := s.listItems(ctx, "cars", limit)
	if err != nil {
		return nil, err
	}
	cars := make([]Car, 0, len(items))
	for _, item := range items {
		cars = append(cars, Car{ID: strconv.FormatInt(item.ID, 10), Name: item.Name})
	}
	return cars, nil
}

func (s *Store) GetCar(ctx context.Context, id int64) (Car, error) {
	item, err := s.getItem(ctx, "cars", id)
	if err != nil {
		return Car{}, err
	}
	return Car{ID: strconv.FormatInt(item.ID, 10), Name: item.Name}, nil
}

func (s *Store) CreateCar(ctx context.Context, name string) (Car, error) {
	item, err := s.createItem(ctx, "cars", name)
	if err != nil {
		return Car{}, err
	}
	return Car{ID: strconv.FormatInt(item.ID, 10), Name: item.Name}, nil
}

func seedMovies(ctx context.Context, db *sql.DB) error {
	var count int
	row := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM movies")
	if err := row.Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	movies := []struct {
		title    string
		year     int
		genre    string
		rating   float64
		director string
	}{
		{"The Matrix", 1999, "Sci-Fi", 8.7, "Lana Wachowski"},
		{"Inception", 2010, "Sci-Fi", 8.8, "Christopher Nolan"},
		{"The Godfather", 1972, "Crime", 9.2, "Francis Ford Coppola"},
		{"Pulp Fiction", 1994, "Crime", 8.9, "Quentin Tarantino"},
		{"Interstellar", 2014, "Sci-Fi", 8.6, "Christopher Nolan"},
		{"The Dark Knight", 2008, "Action", 9.0, "Christopher Nolan"},
		{"Forrest Gump", 1994, "Drama", 8.8, "Robert Zemeckis"},
		{"Fight Club", 1999, "Drama", 8.8, "David Fincher"},
	}
	for _, m := range movies {
		if _, err := db.ExecContext(ctx, "INSERT INTO movies (title, year, genre, rating, director) VALUES (?, ?, ?, ?, ?)", m.title, m.year, m.genre, m.rating, m.director); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListMovies(ctx context.Context, top, skip int, orderby, filter string) ([]Movie, error) {
	query := "SELECT id, title, year, genre, rating, director FROM movies"
	var args []any
	if filter != "" {
		where, filterArgs := buildODataFilter(filter)
		if where != "" {
			query += " WHERE " + where
			args = append(args, filterArgs...)
		}
	}
	if orderby != "" {
		query += " ORDER BY " + sanitizeODataOrderBy(orderby)
	} else {
		query += " ORDER BY id"
	}
	if top > 0 {
		query += " LIMIT ?"
		args = append(args, top)
	}
	if skip > 0 {
		query += " OFFSET ?"
		args = append(args, skip)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var movies []Movie
	for rows.Next() {
		var m Movie
		if err := rows.Scan(&m.ID, &m.Title, &m.Year, &m.Genre, &m.Rating, &m.Director); err != nil {
			return nil, err
		}
		movies = append(movies, m)
	}
	if movies == nil {
		movies = []Movie{}
	}
	return movies, rows.Err()
}

func (s *Store) CountMovies(ctx context.Context, filter string) (int, error) {
	query := "SELECT COUNT(*) FROM movies"
	var args []any
	if filter != "" {
		where, filterArgs := buildODataFilter(filter)
		if where != "" {
			query += " WHERE " + where
			args = append(args, filterArgs...)
		}
	}
	var count int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) GetMovie(ctx context.Context, id int64) (Movie, error) {
	var m Movie
	err := s.db.QueryRowContext(ctx, "SELECT id, title, year, genre, rating, director FROM movies WHERE id = ?", id).Scan(&m.ID, &m.Title, &m.Year, &m.Genre, &m.Rating, &m.Director)
	return m, err
}

func (s *Store) CreateMovie(ctx context.Context, m Movie) (Movie, error) {
	res, err := s.db.ExecContext(ctx, "INSERT INTO movies (title, year, genre, rating, director) VALUES (?, ?, ?, ?, ?)", m.Title, m.Year, m.Genre, m.Rating, m.Director)
	if err != nil {
		return Movie{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Movie{}, err
	}
	m.ID = id
	return m, nil
}

func (s *Store) UpdateMovie(ctx context.Context, id int64, m Movie) (Movie, error) {
	res, err := s.db.ExecContext(ctx, "UPDATE movies SET title = ?, year = ?, genre = ?, rating = ?, director = ? WHERE id = ?", m.Title, m.Year, m.Genre, m.Rating, m.Director, id)
	if err != nil {
		return Movie{}, err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return Movie{}, sql.ErrNoRows
	}
	m.ID = id
	return m, nil
}

func (s *Store) DeleteMovie(ctx context.Context, id int64) error {
	return s.deleteItem(ctx, "movies", id)
}

func (s *Store) listItems(ctx context.Context, table string, limit int) ([]dbItem, error) {
	if _, err := allowTable(table); err != nil {
		return nil, err
	}
	query := fmt.Sprintf("SELECT id, name FROM %s ORDER BY id", table)
	var rows *sql.Rows
	var err error
	if limit > 0 {
		query += " LIMIT ?"
		rows, err = s.db.QueryContext(ctx, query, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []dbItem{}
	for rows.Next() {
		var item dbItem
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) getItem(ctx context.Context, table string, id int64) (dbItem, error) {
	if _, err := allowTable(table); err != nil {
		return dbItem{}, err
	}
	row := s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT id, name FROM %s WHERE id = ?", table), id)
	var item dbItem
	if err := row.Scan(&item.ID, &item.Name); err != nil {
		return dbItem{}, err
	}
	return item, nil
}

func (s *Store) createItem(ctx context.Context, table, name string) (dbItem, error) {
	if _, err := allowTable(table); err != nil {
		return dbItem{}, err
	}
	res, err := s.db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (name) VALUES (?)", table), name)
	if err != nil {
		return dbItem{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return dbItem{}, err
	}
	return dbItem{ID: id, Name: name}, nil
}

func (s *Store) updateItem(ctx context.Context, table string, id int64, name string) (dbItem, error) {
	if _, err := allowTable(table); err != nil {
		return dbItem{}, err
	}
	res, err := s.db.ExecContext(ctx, fmt.Sprintf("UPDATE %s SET name = ? WHERE id = ?", table), name, id)
	if err != nil {
		return dbItem{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return dbItem{}, err
	}
	if affected == 0 {
		return dbItem{}, sql.ErrNoRows
	}
	return dbItem{ID: id, Name: name}, nil
}

func (s *Store) deleteItem(ctx context.Context, table string, id int64) error {
	if _, err := allowTable(table); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE id = ?", table), id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// OData helpers

var odataFieldMap = map[string]string{
	"id": "id", "ID": "id",
	"title": "title", "Title": "title",
	"year": "year", "Year": "year",
	"genre": "genre", "Genre": "genre",
	"rating": "rating", "Rating": "rating",
	"director": "director", "Director": "director",
}

func sanitizeODataOrderBy(raw string) string {
	parts := strings.Split(raw, ",")
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		tokens := strings.Fields(part)
		if len(tokens) == 0 {
			continue
		}
		col, ok := odataFieldMap[tokens[0]]
		if !ok {
			continue
		}
		dir := "ASC"
		if len(tokens) > 1 && strings.EqualFold(tokens[1], "desc") {
			dir = "DESC"
		}
		out = append(out, col+" "+dir)
	}
	if len(out) == 0 {
		return "id ASC"
	}
	return strings.Join(out, ", ")
}

func buildODataFilter(raw string) (string, []any) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}

	// contains(Field,'value')
	if strings.HasPrefix(strings.ToLower(raw), "contains(") {
		inner := raw[len("contains("):]
		inner = strings.TrimSuffix(inner, ")")
		parts := strings.SplitN(inner, ",", 2)
		if len(parts) == 2 {
			col, ok := odataFieldMap[strings.TrimSpace(parts[0])]
			if ok {
				val := strings.Trim(strings.TrimSpace(parts[1]), "'\"")
				return col + " LIKE ?", []any{"%" + val + "%"}
			}
		}
		return "", nil
	}

	// Field op value
	ops := []struct {
		odata string
		sql   string
	}{
		{"ge", ">="}, {"le", "<="}, {"gt", ">"}, {"lt", "<"}, {"ne", "!="}, {"eq", "="},
	}
	for _, op := range ops {
		sep := " " + op.odata + " "
		idx := strings.Index(strings.ToLower(raw), strings.ToLower(sep))
		if idx < 0 {
			continue
		}
		field := strings.TrimSpace(raw[:idx])
		value := strings.TrimSpace(raw[idx+len(sep):])
		col, ok := odataFieldMap[field]
		if !ok {
			return "", nil
		}
		value = strings.Trim(value, "'\"")
		return col + " " + op.sql + " ?", []any{value}
	}
	return "", nil
}

func odataSelectFields(raw string) []string {
	if raw == "" {
		return nil
	}
	var fields []string
	for _, f := range strings.Split(raw, ",") {
		f = strings.TrimSpace(f)
		if _, ok := odataFieldMap[f]; ok {
			fields = append(fields, f)
		}
	}
	return fields
}

func odataMovieMap(m Movie, fields []string) map[string]any {
	full := map[string]any{
		"ID": m.ID, "Title": m.Title, "Year": m.Year,
		"Genre": m.Genre, "Rating": m.Rating, "Director": m.Director,
	}
	if len(fields) == 0 {
		return full
	}
	out := map[string]any{}
	for _, f := range fields {
		if v, ok := full[f]; ok {
			out[f] = v
		}
	}
	return out
}

// OData handlers

func (s *Server) handleOData(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case p == "/odata/" || p == "/odata":
		s.handleODataRoot(w, r)
	case p == "/odata/$metadata":
		s.handleODataMetadata(w, r)
	case strings.Contains(p, "Movies("):
		s.handleODataMovie(w, r)
	case strings.HasPrefix(p, "/odata/Movies"):
		s.handleODataMovies(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleODataRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"@odata.context": "http://localhost:9999/odata/$metadata",
		"value": []map[string]any{
			{"name": "Movies", "kind": "EntitySet", "url": "Movies"},
		},
	})
}

func (s *Server) handleODataMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(odataMetadataCSDL))
}

func (s *Server) handleODataMovies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		top, _ := strconv.Atoi(q.Get("$top"))
		skip, _ := strconv.Atoi(q.Get("$skip"))
		orderby := q.Get("$orderby")
		filter := q.Get("$filter")
		selectRaw := q.Get("$select")
		countReq := strings.EqualFold(q.Get("$count"), "true")

		movies, err := s.store.ListMovies(r.Context(), top, skip, orderby, filter)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		fields := odataSelectFields(selectRaw)
		values := make([]map[string]any, 0, len(movies))
		for _, m := range movies {
			values = append(values, odataMovieMap(m, fields))
		}
		result := map[string]any{
			"@odata.context": "http://localhost:9999/odata/$metadata#Movies",
			"value":          values,
		}
		if countReq {
			count, err := s.store.CountMovies(r.Context(), filter)
			if err == nil {
				result["@odata.count"] = count
			}
		}
		respondJSON(w, http.StatusOK, result)
	case http.MethodPost:
		var input Movie
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if input.Title == "" {
			http.Error(w, "Title is required", http.StatusBadRequest)
			return
		}
		movie, err := s.store.CreateMovie(r.Context(), input)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		result := odataMovieMap(movie, nil)
		result["@odata.context"] = "http://localhost:9999/odata/$metadata#Movies/$entity"
		respondJSON(w, http.StatusCreated, result)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleODataMovie(w http.ResponseWriter, r *http.Request) {
	// Parse /odata/Movies({id}) or /odata/Movies(id)
	path := strings.TrimPrefix(r.URL.Path, "/odata/Movies(")
	path = strings.TrimSuffix(path, ")")
	path = strings.TrimSuffix(path, "/")
	id, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		movie, err := s.store.GetMovie(r.Context(), id)
		if err == sql.ErrNoRows {
			respondJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"code": "404", "message": "Movie not found"}})
			return
		}
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		fields := odataSelectFields(r.URL.Query().Get("$select"))
		result := odataMovieMap(movie, fields)
		result["@odata.context"] = "http://localhost:9999/odata/$metadata#Movies/$entity"
		respondJSON(w, http.StatusOK, result)
	case http.MethodPut, http.MethodPatch:
		existing, err := s.store.GetMovie(r.Context(), id)
		if err == sql.ErrNoRows {
			respondJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"code": "404", "message": "Movie not found"}})
			return
		}
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		var input Movie
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		// PATCH: merge with existing
		if r.Method == http.MethodPatch {
			if input.Title == "" {
				input.Title = existing.Title
			}
			if input.Year == 0 {
				input.Year = existing.Year
			}
			if input.Genre == "" {
				input.Genre = existing.Genre
			}
			if input.Rating == 0 {
				input.Rating = existing.Rating
			}
			if input.Director == "" {
				input.Director = existing.Director
			}
		}
		movie, err := s.store.UpdateMovie(r.Context(), id, input)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		result := odataMovieMap(movie, nil)
		result["@odata.context"] = "http://localhost:9999/odata/$metadata#Movies/$entity"
		respondJSON(w, http.StatusOK, result)
	case http.MethodDelete:
		if err := s.store.DeleteMovie(r.Context(), id); err == sql.ErrNoRows {
			respondJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"code": "404", "message": "Movie not found"}})
			return
		} else if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

const odataMetadataCSDL = `<?xml version="1.0" encoding="utf-8"?>
<edmx:Edmx Version="4.0" xmlns:edmx="http://docs.oasis-open.org/odata/ns/edmx">
  <edmx:DataServices>
    <Schema Namespace="MockMovies" xmlns="http://docs.oasis-open.org/odata/ns/edm">
      <EntityType Name="Movie">
        <Key>
          <PropertyRef Name="ID"/>
        </Key>
        <Property Name="ID" Type="Edm.Int64" Nullable="false"/>
        <Property Name="Title" Type="Edm.String" Nullable="false"/>
        <Property Name="Year" Type="Edm.Int32" Nullable="false"/>
        <Property Name="Genre" Type="Edm.String" Nullable="false"/>
        <Property Name="Rating" Type="Edm.Double" Nullable="false"/>
        <Property Name="Director" Type="Edm.String" Nullable="false"/>
      </EntityType>
      <EntityContainer Name="Container">
        <EntitySet Name="Movies" EntityType="MockMovies.Movie"/>
      </EntityContainer>
    </Schema>
  </edmx:DataServices>
</edmx:Edmx>`

const openAPISpec = `{
  "openapi": "3.0.0",
  "info": {"title": "Mock Petstore", "version": "1.0.0"},
  "servers": [{"url": "http://localhost:9999/openapi"}],
  "paths": {
    "/pets": {
      "get": {
        "operationId": "listPets",
        "summary": "List pets",
        "parameters": [
          {"name": "limit", "in": "query", "schema": {"type": "integer"}}
        ],
        "responses": {
          "200": {
            "description": "ok",
            "content": {
              "application/json": {
                "schema": {
                  "type": "array",
                  "items": {
                    "type": "object",
                    "properties": {
                      "id": {"type": "string"},
                      "name": {"type": "string"}
                    }
                  }
                }
              }
            }
          }
        }
      },
      "post": {
        "operationId": "createPet",
        "summary": "Create pet",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {"name": {"type": "string"}},
                "required": ["name"]
              }
            }
          }
        },
        "responses": {
          "201": {
            "description": "created",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "id": {"type": "string"},
                    "name": {"type": "string"}
                  }
                }
              }
            }
          }
        }
      }
    },
    "/pets/{id}": {
      "get": {
        "operationId": "getPet",
        "summary": "Get pet",
        "parameters": [
          {"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {
            "description": "ok",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "id": {"type": "string"},
                    "name": {"type": "string"}
                  }
                }
              }
            }
          }
        }
      },
      "put": {
        "operationId": "updatePet",
        "summary": "Update pet",
        "parameters": [
          {"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {"name": {"type": "string"}},
                "required": ["name"]
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "updated",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "id": {"type": "string"},
                    "name": {"type": "string"}
                  }
                }
              }
            }
          }
        }
      },
      "delete": {
        "operationId": "deletePet",
        "summary": "Delete pet",
        "parameters": [
          {"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}
        ],
        "responses": {
          "204": {
            "description": "deleted"
          }
        }
      }
    }
  }
}
`

const swagger2Spec = `{
  "swagger": "2.0",
  "info": {"title": "Mock Dinosaurs", "version": "1.0.0"},
  "host": "localhost:9999",
  "schemes": ["http"],
  "basePath": "/swagger",
  "paths": {
    "/dinosaurs": {
      "get": {
        "operationId": "listDinosaurs",
        "summary": "List dinosaurs",
        "parameters": [
          {"name": "limit", "in": "query", "type": "integer", "description": "Max items to return"}
        ],
        "responses": {
          "200": {
            "description": "ok",
            "schema": {
              "type": "array",
              "items": {"$ref": "#/definitions/Dinosaur"}
            }
          }
        }
      },
      "post": {
        "operationId": "createDinosaur",
        "summary": "Create dinosaur",
        "parameters": [
          {"name": "body", "in": "body", "schema": {"$ref": "#/definitions/DinosaurInput"}}
        ],
        "responses": {
          "201": {
            "description": "created",
            "schema": {"$ref": "#/definitions/Dinosaur"}
          }
        }
      }
    },
    "/dinosaurs/{id}": {
      "get": {
        "operationId": "getDinosaur",
        "summary": "Get dinosaur",
        "parameters": [
          {"name": "id", "in": "path", "required": true, "type": "string"}
        ],
        "responses": {
          "200": {
            "description": "ok",
            "schema": {"$ref": "#/definitions/Dinosaur"}
          }
        }
      },
      "put": {
        "operationId": "updateDinosaur",
        "summary": "Update dinosaur",
        "parameters": [
          {"name": "id", "in": "path", "required": true, "type": "string"},
          {"name": "body", "in": "body", "schema": {"$ref": "#/definitions/DinosaurInput"}}
        ],
        "responses": {
          "200": {
            "description": "updated",
            "schema": {"$ref": "#/definitions/Dinosaur"}
          }
        }
      },
      "delete": {
        "operationId": "deleteDinosaur",
        "summary": "Delete dinosaur",
        "parameters": [
          {"name": "id", "in": "path", "required": true, "type": "string"}
        ],
        "responses": {
          "204": {"description": "deleted"}
        }
      }
    }
  },
  "definitions": {
    "Dinosaur": {
      "type": "object",
      "properties": {
        "id": {"type": "string"},
        "name": {"type": "string"}
      }
    },
    "DinosaurInput": {
      "type": "object",
      "properties": {
        "name": {"type": "string"}
      },
      "required": ["name"]
    }
  }
}
`

const wsdlSpec = `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://schemas.xmlsoap.org/wsdl/"
  xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
  xmlns:tns="http://example.com/plants"
  targetNamespace="http://example.com/plants">
  <service name="PlantSoapService">
    <port name="PlantSoapPort" binding="tns:PlantSoapBinding">
      <soap:address location="http://localhost:9999/wdsl/soap"/>
    </port>
  </service>
  <binding name="PlantSoapBinding" type="tns:PlantSoapPortType">
    <soap:binding style="document" transport="http://schemas.xmlsoap.org/soap/http"/>
    <operation name="ListPlants">
      <soap:operation soapAction="urn:ListPlants"/>
      <input><soap:body use="literal"/></input>
      <output><soap:body use="literal"/></output>
    </operation>
    <operation name="GetPlant">
      <soap:operation soapAction="urn:GetPlant"/>
      <input><soap:body use="literal"/></input>
      <output><soap:body use="literal"/></output>
    </operation>
    <operation name="CreatePlant">
      <soap:operation soapAction="urn:CreatePlant"/>
      <input><soap:body use="literal"/></input>
      <output><soap:body use="literal"/></output>
    </operation>
    <operation name="UpdatePlant">
      <soap:operation soapAction="urn:UpdatePlant"/>
      <input><soap:body use="literal"/></input>
      <output><soap:body use="literal"/></output>
    </operation>
    <operation name="DeletePlant">
      <soap:operation soapAction="urn:DeletePlant"/>
      <input><soap:body use="literal"/></input>
      <output><soap:body use="literal"/></output>
    </operation>
  </binding>
</definitions>
`

const graphQLSDL = `schema {
  query: Query
  mutation: Mutation
}

type Query {
  listCars(limit: Int): [Car!]!
  getCar(id: ID!): Car
}

type Mutation {
  createCar(name: String!): Car!
}

type Car {
  id: ID!
  name: String!
}
`

// JSON-RPC / OpenRPC handlers

func (s *Server) handleOpenRPCSpec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(openRPCSpec))
}

func (s *Server) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		JSONRPC string         `json:"jsonrpc"`
		Method  string         `json:"method"`
		Params  map[string]any `json:"params"`
		ID      any            `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSONRPCError(w, nil, -32700, "Parse error")
		return
	}

	switch req.Method {
	case "rpc.discover":
		// Return the OpenRPC spec as the result.
		var spec any
		_ = json.Unmarshal([]byte(openRPCSpec), &spec)
		respondJSONRPCResult(w, req.ID, spec)
	case "add":
		a, b, err := extractAB(req.Params)
		if err != nil {
			respondJSONRPCError(w, req.ID, -32602, err.Error())
			return
		}
		respondJSONRPCResult(w, req.ID, a+b)
	case "subtract":
		a, b, err := extractAB(req.Params)
		if err != nil {
			respondJSONRPCError(w, req.ID, -32602, err.Error())
			return
		}
		respondJSONRPCResult(w, req.ID, a-b)
	case "multiply":
		a, b, err := extractAB(req.Params)
		if err != nil {
			respondJSONRPCError(w, req.ID, -32602, err.Error())
			return
		}
		respondJSONRPCResult(w, req.ID, a*b)
	case "divide":
		a, b, err := extractAB(req.Params)
		if err != nil {
			respondJSONRPCError(w, req.ID, -32602, err.Error())
			return
		}
		if b == 0 {
			respondJSONRPCError(w, req.ID, -32602, "division by zero")
			return
		}
		respondJSONRPCResult(w, req.ID, a/b)
	default:
		respondJSONRPCError(w, req.ID, -32601, "Method not found")
	}
}

func extractAB(params map[string]any) (float64, float64, error) {
	a, ok := toFloat64(params["a"])
	if !ok {
		return 0, 0, fmt.Errorf("missing or invalid param 'a'")
	}
	b, ok := toFloat64(params["b"])
	if !ok {
		return 0, 0, fmt.Errorf("missing or invalid param 'b'")
	}
	return a, b, nil
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func respondJSONRPCResult(w http.ResponseWriter, id any, result any) {
	respondJSON(w, http.StatusOK, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

func respondJSONRPCError(w http.ResponseWriter, id any, code int, message string) {
	respondJSON(w, http.StatusOK, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": message},
	})
}

const openRPCSpec = `{
  "openrpc": "1.2.6",
  "info": { "title": "Mock Calculator", "version": "1.0.0" },
  "servers": [{ "name": "local", "url": "http://localhost:9999/jsonrpc" }],
  "methods": [
    {
      "name": "add",
      "summary": "Add two numbers",
      "params": [
        { "name": "a", "required": true, "schema": { "type": "number" } },
        { "name": "b", "required": true, "schema": { "type": "number" } }
      ],
      "result": { "name": "result", "schema": { "type": "number" } }
    },
    {
      "name": "subtract",
      "summary": "Subtract b from a",
      "params": [
        { "name": "a", "required": true, "schema": { "type": "number" } },
        { "name": "b", "required": true, "schema": { "type": "number" } }
      ],
      "result": { "name": "result", "schema": { "type": "number" } }
    },
    {
      "name": "multiply",
      "summary": "Multiply two numbers",
      "params": [
        { "name": "a", "required": true, "schema": { "type": "number" } },
        { "name": "b", "required": true, "schema": { "type": "number" } }
      ],
      "result": { "name": "result", "schema": { "type": "number" } }
    },
    {
      "name": "divide",
      "summary": "Divide a by b",
      "params": [
        { "name": "a", "required": true, "schema": { "type": "number" } },
        { "name": "b", "required": true, "schema": { "type": "number" } }
      ],
      "result": { "name": "result", "schema": { "type": "number" } }
    }
  ]
}`
