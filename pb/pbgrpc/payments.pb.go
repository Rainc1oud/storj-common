// Code generated by protoc-gen-gogo. DO NOT EDIT.
// source: payments.proto

package pbgrpc

import (
	context "context"

	grpc "google.golang.org/grpc"

	. "storj.io/common/pb"
)

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConn

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion4

// PaymentsClient is the client API for Payments service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type PaymentsClient interface {
	PrepareInvoiceRecords(ctx context.Context, in *PrepareInvoiceRecordsRequest, opts ...grpc.CallOption) (*PrepareInvoiceRecordsResponse, error)
	ApplyInvoiceRecords(ctx context.Context, in *ApplyInvoiceRecordsRequest, opts ...grpc.CallOption) (*ApplyInvoiceRecordsResponse, error)
	ApplyInvoiceCoupons(ctx context.Context, in *ApplyInvoiceCouponsRequest, opts ...grpc.CallOption) (*ApplyInvoiceCouponsResponse, error)
	ApplyInvoiceCredits(ctx context.Context, in *ApplyInvoiceCreditsRequest, opts ...grpc.CallOption) (*ApplyInvoiceCreditsResponse, error)
	CreateInvoices(ctx context.Context, in *CreateInvoicesRequest, opts ...grpc.CallOption) (*CreateInvoicesResponse, error)
}

type paymentsClient struct {
	cc *grpc.ClientConn
}

func NewPaymentsClient(cc *grpc.ClientConn) PaymentsClient {
	return &paymentsClient{cc}
}

func (c *paymentsClient) PrepareInvoiceRecords(ctx context.Context, in *PrepareInvoiceRecordsRequest, opts ...grpc.CallOption) (*PrepareInvoiceRecordsResponse, error) {
	out := new(PrepareInvoiceRecordsResponse)
	err := c.cc.Invoke(ctx, "/nodestats.Payments/PrepareInvoiceRecords", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *paymentsClient) ApplyInvoiceRecords(ctx context.Context, in *ApplyInvoiceRecordsRequest, opts ...grpc.CallOption) (*ApplyInvoiceRecordsResponse, error) {
	out := new(ApplyInvoiceRecordsResponse)
	err := c.cc.Invoke(ctx, "/nodestats.Payments/ApplyInvoiceRecords", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *paymentsClient) ApplyInvoiceCoupons(ctx context.Context, in *ApplyInvoiceCouponsRequest, opts ...grpc.CallOption) (*ApplyInvoiceCouponsResponse, error) {
	out := new(ApplyInvoiceCouponsResponse)
	err := c.cc.Invoke(ctx, "/nodestats.Payments/ApplyInvoiceCoupons", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *paymentsClient) ApplyInvoiceCredits(ctx context.Context, in *ApplyInvoiceCreditsRequest, opts ...grpc.CallOption) (*ApplyInvoiceCreditsResponse, error) {
	out := new(ApplyInvoiceCreditsResponse)
	err := c.cc.Invoke(ctx, "/nodestats.Payments/ApplyInvoiceCredits", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *paymentsClient) CreateInvoices(ctx context.Context, in *CreateInvoicesRequest, opts ...grpc.CallOption) (*CreateInvoicesResponse, error) {
	out := new(CreateInvoicesResponse)
	err := c.cc.Invoke(ctx, "/nodestats.Payments/CreateInvoices", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PaymentsServer is the server API for Payments service.
type PaymentsServer interface {
	PrepareInvoiceRecords(context.Context, *PrepareInvoiceRecordsRequest) (*PrepareInvoiceRecordsResponse, error)
	ApplyInvoiceRecords(context.Context, *ApplyInvoiceRecordsRequest) (*ApplyInvoiceRecordsResponse, error)
	ApplyInvoiceCoupons(context.Context, *ApplyInvoiceCouponsRequest) (*ApplyInvoiceCouponsResponse, error)
	ApplyInvoiceCredits(context.Context, *ApplyInvoiceCreditsRequest) (*ApplyInvoiceCreditsResponse, error)
	CreateInvoices(context.Context, *CreateInvoicesRequest) (*CreateInvoicesResponse, error)
}

func RegisterPaymentsServer(s *grpc.Server, srv PaymentsServer) {
	s.RegisterService(&_Payments_serviceDesc, srv)
}

func _Payments_PrepareInvoiceRecords_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(PrepareInvoiceRecordsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PaymentsServer).PrepareInvoiceRecords(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/nodestats.Payments/PrepareInvoiceRecords",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PaymentsServer).PrepareInvoiceRecords(ctx, req.(*PrepareInvoiceRecordsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Payments_ApplyInvoiceRecords_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ApplyInvoiceRecordsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PaymentsServer).ApplyInvoiceRecords(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/nodestats.Payments/ApplyInvoiceRecords",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PaymentsServer).ApplyInvoiceRecords(ctx, req.(*ApplyInvoiceRecordsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Payments_ApplyInvoiceCoupons_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ApplyInvoiceCouponsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PaymentsServer).ApplyInvoiceCoupons(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/nodestats.Payments/ApplyInvoiceCoupons",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PaymentsServer).ApplyInvoiceCoupons(ctx, req.(*ApplyInvoiceCouponsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Payments_ApplyInvoiceCredits_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ApplyInvoiceCreditsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PaymentsServer).ApplyInvoiceCredits(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/nodestats.Payments/ApplyInvoiceCredits",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PaymentsServer).ApplyInvoiceCredits(ctx, req.(*ApplyInvoiceCreditsRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Payments_CreateInvoices_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateInvoicesRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(PaymentsServer).CreateInvoices(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/nodestats.Payments/CreateInvoices",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(PaymentsServer).CreateInvoices(ctx, req.(*CreateInvoicesRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _Payments_serviceDesc = grpc.ServiceDesc{
	ServiceName: "nodestats.Payments",
	HandlerType: (*PaymentsServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "PrepareInvoiceRecords",
			Handler:    _Payments_PrepareInvoiceRecords_Handler,
		},
		{
			MethodName: "ApplyInvoiceRecords",
			Handler:    _Payments_ApplyInvoiceRecords_Handler,
		},
		{
			MethodName: "ApplyInvoiceCoupons",
			Handler:    _Payments_ApplyInvoiceCoupons_Handler,
		},
		{
			MethodName: "ApplyInvoiceCredits",
			Handler:    _Payments_ApplyInvoiceCredits_Handler,
		},
		{
			MethodName: "CreateInvoices",
			Handler:    _Payments_CreateInvoices_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "payments.proto",
}
